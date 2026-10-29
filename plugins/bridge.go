package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// RuntimeConfig runs SDK plugins (@escalated-dev/plugin-sdk) through the
// @escalated-dev/plugin-runtime Node process, spoken to as JSON-RPC 2.0 over
// its stdin and stdout. The runtime is started on first use; when it cannot
// start (no Node, no package) the host carries on without SDK plugins and
// retries with exponential backoff, capped at five minutes.
type RuntimeConfig struct {
	// Enabled turns the bridge on. Off by default.
	Enabled bool
	// Command is the executable. Default "node".
	Command string
	// Args are its arguments. Default
	// node_modules/@escalated-dev/plugin-runtime/build/bin/escalated-plugins.js,
	// which loads the plugins installed under Dir's node_modules.
	Args []string
	// Dir is the project directory the runtime runs in. Default the
	// process's working directory.
	Dir string
	// Env adds environment variables (KEY=value) to the host's own.
	Env []string
}

// DefaultRuntimeScript is the runtime's entry point relative to the project
// directory.
const DefaultRuntimeScript = "node_modules/@escalated-dev/plugin-runtime/build/bin/escalated-plugins.js"

const (
	protocolVersion   = "1.0"
	handshakeTimeout  = 15 * time.Second
	maxMessageBytes   = 10 << 20
	maxRestartBackoff = 5 * time.Minute
)

// ErrRuntimeUnavailable is returned while the plugin runtime is not running.
var ErrRuntimeUnavailable = errors.New("the plugin runtime is not available")

// scope is the dispatch a runtime call serves: the plugins it may act for
// and the request behind it.
type scope struct {
	plugins []string
	request *http.Request
}

// scopeKey names a host call: the runtime process and the call's JSON-RPC
// id, which the runtime stamps on the ctx.* calls it makes while serving it
// (host_call_id).
type scopeKey struct {
	proc *runtimeProc
	id   int64
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type serveFunc func(ctx context.Context, plugin string, method string, params map[string]any) (any, error)

type bridge struct {
	cfg     RuntimeConfig
	version string
	logger  *log.Logger
	serve   serveFunc

	mu           sync.Mutex
	proc         *runtimeProc
	failures     int
	retryAt      time.Time
	cached       []*entry
	httpContract bool

	scopesMu sync.Mutex
	scopes   map[scopeKey]scope
}

type runtimeProc struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[string]chan rpcMessage
	nextID    int64

	done chan struct{}
}

func newBridge(cfg RuntimeConfig, version string, logger *log.Logger, serve serveFunc) *bridge {
	if cfg.Command == "" {
		cfg.Command = "node"
	}
	if len(cfg.Args) == 0 {
		cfg.Args = []string{DefaultRuntimeScript}
	}
	return &bridge{cfg: cfg, version: version, logger: logger, serve: serve, scopes: map[scopeKey]scope{}}
}

func (b *bridge) logf(format string, args ...any) {
	b.logger.Printf("escalated plugins: runtime: "+format, args...)
}

func (p *runtimeProc) alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// ensure returns a running, handshaken runtime, starting one when the last
// has died and the backoff has passed.
func (b *bridge) ensure() (*runtimeProc, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.proc != nil && b.proc.alive() {
		return b.proc, nil
	}
	if time.Now().Before(b.retryAt) {
		return nil, ErrRuntimeUnavailable
	}
	p, err := b.start()
	if err != nil {
		b.failures++
		backoff := time.Second << min(b.failures-1, 9)
		if backoff > maxRestartBackoff {
			backoff = maxRestartBackoff
		}
		b.retryAt = time.Now().Add(backoff)
		b.logf("could not start (retrying in %s): %v", backoff, err)
		return nil, ErrRuntimeUnavailable
	}
	b.failures = 0
	b.proc = p
	return p, nil
}

func (b *bridge) start() (*runtimeProc, error) {
	cmd := exec.Command(b.cfg.Command, b.cfg.Args...)
	cmd.Dir = b.cfg.Dir
	cmd.Env = append(os.Environ(), b.cfg.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &runtimeProc{cmd: cmd, stdin: stdin, pending: map[string]chan rpcMessage{}, done: make(chan struct{})}

	go func() {
		s := bufio.NewScanner(stderr)
		s.Buffer(make([]byte, 64*1024), maxMessageBytes)
		for s.Scan() {
			b.logf("%s", s.Text())
		}
	}()
	go b.read(p, stdout)

	kill := func(err error) (*runtimeProc, error) {
		p.kill()
		return nil, err
	}
	raw, err := b.send(p, "handshake", map[string]any{"protocol_version": protocolVersion, "host": "escalated-go", "host_version": b.version}, handshakeTimeout)
	if err != nil {
		return kill(fmt.Errorf("handshake: %w", err))
	}
	var hs struct {
		Compatible           bool  `json:"compatible"`
		HTTPContractVersions []int `json:"http_contract_versions"`
	}
	if err := json.Unmarshal(raw, &hs); err != nil || !hs.Compatible {
		return kill(fmt.Errorf("handshake: the runtime is not compatible with protocol %s", protocolVersion))
	}
	contract := false
	for _, v := range hs.HTTPContractVersions {
		if v == 1 {
			contract = true
		}
	}

	raw, err = b.send(p, "manifest", map[string]any{}, handshakeTimeout)
	if err != nil {
		return kill(fmt.Errorf("manifest: %w", err))
	}
	var manifests map[string]sdkManifest
	if err := json.Unmarshal(raw, &manifests); err != nil {
		return kill(fmt.Errorf("manifest: %w", err))
	}
	b.cached = b.sdkEntries(manifests)
	b.httpContract = contract
	return p, nil
}

// read delivers responses to their callers and serves the runtime's ctx.*
// requests, each in its own goroutine so they are answered while the host
// waits for its own call.
func (b *bridge) read(p *runtimeProc, stdout io.Reader) {
	s := bufio.NewScanner(stdout)
	s.Buffer(make([]byte, 64*1024), maxMessageBytes)
	for s.Scan() {
		line := s.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			b.logf("ignored a malformed message")
			continue
		}
		if msg.Method != "" {
			if len(msg.ID) > 0 && string(msg.ID) != "null" {
				go b.answer(p, msg)
			}
			continue
		}
		p.pendingMu.Lock()
		ch := p.pending[string(msg.ID)]
		delete(p.pending, string(msg.ID))
		p.pendingMu.Unlock()
		if ch != nil {
			ch <- msg
		}
	}
	if err := s.Err(); err != nil {
		b.logf("stopped reading: %v", err)
	}
	p.kill()
}

func (p *runtimeProc) kill() {
	p.pendingMu.Lock()
	select {
	case <-p.done:
		p.pendingMu.Unlock()
		return
	default:
	}
	close(p.done)
	p.pendingMu.Unlock()
	_ = p.stdin.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	go func() { _ = p.cmd.Wait() }()
}

func (p *runtimeProc) write(msg any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(b) > maxMessageBytes {
		return fmt.Errorf("message of %d bytes is over the 10 MB limit", len(b))
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_, err = p.stdin.Write(append(b, '\n'))
	return err
}

// nextCallID reserves the JSON-RPC id of the next call to p.
func (p *runtimeProc) nextCallID() int64 {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	p.nextID++
	return p.nextID
}

// send makes one call and waits for its answer.
func (b *bridge) send(p *runtimeProc, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	return b.sendAs(p, p.nextCallID(), method, params, timeout)
}

// sendAs makes one call with the id n and waits for its answer.
func (b *bridge) sendAs(p *runtimeProc, n int64, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	id := fmt.Sprint(n)
	p.pendingMu.Lock()
	ch := make(chan rpcMessage, 1)
	p.pending[id] = ch
	p.pendingMu.Unlock()
	forget := func() {
		p.pendingMu.Lock()
		delete(p.pending, id)
		p.pendingMu.Unlock()
	}

	if err := p.write(map[string]any{"jsonrpc": "2.0", "id": n, "method": method, "params": params}); err != nil {
		forget()
		return nil, err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case msg := <-ch:
		if msg.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, msg.Error.Message)
		}
		if len(msg.Result) == 0 {
			return json.RawMessage("null"), nil
		}
		return msg.Result, nil
	case <-timer.C:
		forget()
		return nil, fmt.Errorf("%s timed out after %s", method, timeout)
	case <-p.done:
		forget()
		return nil, fmt.Errorf("%s: the runtime stopped", method)
	}
}

// call makes a call on behalf of a dispatch, starting the runtime when
// needed. The dispatch is remembered under the call's id until it returns, so
// the runtime's ctx.* calls for it (stamped with that id) act for its plugins
// and its request.
func (b *bridge) call(ctx context.Context, sc scope, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	p, err := b.ensure()
	if err != nil {
		return nil, err
	}
	key := scopeKey{proc: p, id: p.nextCallID()}
	b.scopesMu.Lock()
	b.scopes[key] = sc
	b.scopesMu.Unlock()
	defer func() {
		b.scopesMu.Lock()
		delete(b.scopes, key)
		b.scopesMu.Unlock()
	}()
	if deadline, ok := ctx.Deadline(); ok {
		if left := time.Until(deadline); left < timeout && left > 0 {
			timeout = left
		}
	}
	return b.sendAs(p, key.id, method, params, timeout)
}

// entries are the SDK plugins the runtime reports, starting it on first use.
// The last list stays while the runtime is down, so the admin screen does not
// lose them.
func (b *bridge) entries(_ context.Context) []*entry {
	_, _ = b.ensure()
	return b.known()
}

// known are the SDK plugins last reported, without starting anything.
func (b *bridge) known() []*entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cached
}

func (b *bridge) supportsHTTP() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.httpContract
}

func (b *bridge) close() {
	b.mu.Lock()
	p := b.proc
	b.proc = nil
	b.mu.Unlock()
	if p != nil {
		p.kill()
	}
}

// resolve decides which plugin a runtime ctx.* call acts for, and the
// request it acts in. A call stamped with the id of a host call in flight
// (host_call_id) belongs to that call: it acts for one of the plugins that
// call serves, in that call's request, whatever else is in flight. Otherwise
// (an older runtime, or a call that outlived its host call) it must name a
// plugin the runtime is being asked to serve right now, and acts in a request
// only when every call in flight for that plugin shares it. A call naming any
// other plugin is refused, never served as the plugin it names. A call that
// names none acts for the plugin being served when there is exactly one.
func (b *bridge) resolve(p *runtimeProc, named string, hostCall *int64) (string, *http.Request, error) {
	b.scopesMu.Lock()
	defer b.scopesMu.Unlock()
	if hostCall != nil {
		if sc, ok := b.scopes[scopeKey{proc: p, id: *hostCall}]; ok {
			if named == "" && len(sc.plugins) == 1 {
				return sc.plugins[0], sc.request, nil
			}
			for _, name := range sc.plugins {
				if name == named {
					return named, sc.request, nil
				}
			}
			return "", nil, fmt.Errorf("host call %d is not serving plugin %q", *hostCall, named)
		}
	}
	if named == "" {
		if len(b.scopes) == 1 {
			for _, sc := range b.scopes {
				if len(sc.plugins) == 1 {
					return sc.plugins[0], sc.request, nil
				}
			}
		}
		return "", nil, fmt.Errorf("the call names no plugin")
	}
	var matched []scope
	for _, sc := range b.scopes {
		for _, name := range sc.plugins {
			if name == named {
				matched = append(matched, sc)
				break
			}
		}
	}
	if len(matched) == 0 {
		return "", nil, fmt.Errorf("no dispatch is serving plugin %q", named)
	}
	r := matched[0].request
	for _, sc := range matched[1:] {
		if sc.request != r {
			r = nil
		}
	}
	return named, r, nil
}

func (b *bridge) answer(p *runtimeProc, msg rpcMessage) {
	var params map[string]any
	if len(msg.Params) > 0 {
		params = asRecord(decodeJSON(msg.Params))
	}
	if params == nil {
		params = map[string]any{}
	}
	named, _ := params["plugin"].(string)
	hostCall := hostCallID(params["host_call_id"])
	delete(params, "host_call_id")
	reply := map[string]any{"jsonrpc": "2.0", "id": msg.ID}
	plugin, r, err := b.resolve(p, named, hostCall)
	var result any
	if err == nil {
		ctx, cancel := context.WithTimeout(withRequest(context.Background(), r), webhookTimeout)
		result, err = b.serve(ctx, plugin, msg.Method, params)
		cancel()
	}
	if err != nil {
		reply["error"] = rpcError{Code: -32000, Message: err.Error()}
	} else {
		reply["result"] = result
	}
	if err := p.write(reply); err != nil {
		b.logf("answering %s: %v", msg.Method, err)
	}
}

// hostCallID reads a ctx.* call's host_call_id: the id of the host call it
// serves, as this bridge sent it (a JSON number). Nil when absent or not one.
func hostCallID(v any) *int64 {
	var n int64
	switch id := v.(type) {
	case float64:
		if id != float64(int64(id)) {
			return nil
		}
		n = int64(id)
	case json.Number:
		parsed, err := id.Int64()
		if err != nil {
			return nil
		}
		n = parsed
	case string:
		if _, err := fmt.Sscan(id, &n); err != nil {
			return nil
		}
	default:
		return nil
	}
	return &n
}

// sdkManifest is a plugin as the runtime's manifest method describes it
// (definePlugin's toManifest()).
type sdkManifest struct {
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Description string        `json:"description"`
	Config      []ConfigField `json:"config"`
	Pages       []Page        `json:"pages"`
	Components  []Component   `json:"components"`
	Widgets     []Widget      `json:"widgets"`
	ActionHooks []string      `json:"actionHooks"`
	FilterHooks []struct {
		Hook     string `json:"hook"`
		Priority *int   `json:"priority"`
	} `json:"filterHooks"`
	Endpoints []struct {
		Method     string `json:"method"`
		Path       string `json:"path"`
		Capability string `json:"capability"`
	} `json:"endpoints"`
	Webhooks []struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	} `json:"webhooks"`
}

func (b *bridge) sdkEntries(manifests map[string]sdkManifest) []*entry {
	names := make([]string, 0, len(manifests))
	for name := range manifests {
		names = append(names, name)
	}
	sort.Strings(names)
	seen := map[string]string{}
	var out []*entry
	for _, key := range names {
		m := manifests[key]
		name := m.Name
		if name == "" {
			name = key
		}
		e := &entry{
			name:   name,
			slug:   Slug(name),
			source: SourceComposer,
			sdk:    true,
			manifest: Manifest{
				Name: name, Version: m.Version, Description: m.Description,
				Config: m.Config, Pages: m.Pages, Components: m.Components, Widgets: m.Widgets,
			},
			actionHooks: map[string]bool{},
			sdkFilters:  map[string]int{},
		}
		if other, ok := seen[e.slug]; ok {
			b.logf("plugins %q and %q share the slug %q; %q is not loaded", other, name, e.slug, name)
			continue
		}
		seen[e.slug] = name
		for _, hook := range m.ActionHooks {
			e.actionHooks[hook] = true
		}
		for _, f := range m.FilterHooks {
			priority := 10
			if f.Priority != nil {
				priority = *f.Priority
			}
			e.sdkFilters[f.Hook] = priority
		}
		for _, ep := range m.Endpoints {
			r, err := parseRoute(name, ep.Method, ep.Path, ep.Capability, nil)
			if err != nil {
				b.logf("%v; skipped", err)
				continue
			}
			e.endpoints = append(e.endpoints, r)
		}
		for _, wh := range m.Webhooks {
			r, err := parseRoute(name, wh.Method, wh.Path, "", nil)
			if err != nil {
				b.logf("%v; skipped", err)
				continue
			}
			e.webhooks = append(e.webhooks, r)
		}
		out = append(out, e)
	}
	return out
}
