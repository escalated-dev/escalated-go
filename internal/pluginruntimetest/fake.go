// Package pluginruntimetest is a stand-in for @escalated-dev/plugin-runtime:
// the same JSON-RPC 2.0 over stdio, answered by Go so the bridge can be tested
// without Node. A test binary becomes the runtime when its TestMain sees
// EnvRun and calls Serve; the plugins it reports come from EnvManifest.
//
// What it does, per method:
//
//   - handshake: compatible, http_contract_versions [1] (none with EnvNoHTTP).
//   - manifest: the EnvManifest JSON.
//   - action: for every plugin whose manifest lists the hook (it ignores the
//     plugins allow-list, as the real runtime does today), ctx.store.insert into
//     the "received" collection {hook, event, plugins}, acting for that plugin.
//   - filter: a string value gets "|sdk" appended; hook "fail" is an error.
//   - activate, deactivate: ctx.store.insert into "lifecycle" {event}.
//   - endpoint, webhook: by declared path. /echo/:id returns the request it
//     was sent; /boom fails; /envelope returns an httpResponse() envelope;
//     /config returns ctx.config.get("api_key"); /Settings returns page props;
//     /spy reads ctx.store.get acting as the plugin named in the query's
//     "as", to show the host refuses it; /ctx makes the ctx.* call its body
//     names ({method, params}) and returns {result} or {error}.
package pluginruntimetest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Environment variables the stand-in reads.
const (
	EnvRun      = "ESCALATED_FAKE_PLUGIN_RUNTIME"
	EnvManifest = "ESCALATED_FAKE_PLUGIN_MANIFEST"
	EnvNoHTTP   = "ESCALATED_FAKE_PLUGIN_NO_HTTP"
)

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type runtime struct {
	out       io.Writer
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan message
	next      int
	manifests map[string]map[string]any
}

// Serve runs the stand-in until stdin closes.
func Serve(in io.Reader, out io.Writer) {
	rt := &runtime{out: out, pending: map[string]chan message{}, manifests: map[string]map[string]any{}}
	_ = json.Unmarshal([]byte(os.Getenv(EnvManifest)), &rt.manifests)
	fmt.Fprintln(os.Stderr, "fake plugin runtime: ready")

	s := bufio.NewScanner(in)
	s.Buffer(make([]byte, 64*1024), 10<<20)
	var wg sync.WaitGroup
	for s.Scan() {
		var msg message
		if err := json.Unmarshal(s.Bytes(), &msg); err != nil {
			continue
		}
		if msg.Method != "" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rt.handle(msg)
			}()
			continue
		}
		rt.pendingMu.Lock()
		ch := rt.pending[string(msg.ID)]
		delete(rt.pending, string(msg.ID))
		rt.pendingMu.Unlock()
		if ch != nil {
			ch <- msg
		}
	}
	wg.Wait()
}

func (rt *runtime) write(v any) {
	b, _ := json.Marshal(v)
	rt.writeMu.Lock()
	defer rt.writeMu.Unlock()
	_, _ = rt.out.Write(append(b, '\n'))
}

// call asks the host for a ctx.* method and waits for the answer.
func (rt *runtime) call(method string, params map[string]any) (any, error) {
	rt.pendingMu.Lock()
	rt.next++
	id := rt.next
	ch := make(chan message, 1)
	rt.pending[fmt.Sprint(id)] = ch
	rt.pendingMu.Unlock()
	rt.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	msg := <-ch
	if msg.Error != nil {
		return nil, fmt.Errorf("%s", msg.Error.Message)
	}
	var v any
	_ = json.Unmarshal(msg.Result, &v)
	return v, nil
}

func (rt *runtime) handle(msg message) {
	var params map[string]any
	_ = json.Unmarshal(msg.Params, &params)
	result, err := rt.dispatch(msg.Method, params)
	if len(msg.ID) == 0 {
		return
	}
	if err != nil {
		rt.write(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32000, "message": err.Error()}})
		return
	}
	rt.write(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
}

func (rt *runtime) declares(plugin, list, value string) bool {
	items, _ := rt.manifests[plugin][list].([]any)
	for _, item := range items {
		if item == value {
			return true
		}
		if m, ok := item.(map[string]any); ok && m["hook"] == value {
			return true
		}
	}
	return false
}

func (rt *runtime) dispatch(method string, p map[string]any) (any, error) {
	switch method {
	case "handshake":
		versions := []int{1}
		if os.Getenv(EnvNoHTTP) != "" {
			versions = []int{}
		}
		return map[string]any{"protocol_version": "1.0", "runtime_version": "fake", "compatible": p["protocol_version"] == "1.0", "http_contract_versions": versions}, nil
	case "manifest":
		return rt.manifests, nil
	case "ping":
		return map[string]any{"pong": true}, nil
	case "action":
		hook, _ := p["hook"].(string)
		for name := range rt.manifests {
			if !rt.declares(name, "actionHooks", hook) {
				continue
			}
			// Errors are what the host refuses; the runtime carries on, as
			// the real one does with Promise.allSettled.
			_, _ = rt.call("ctx.store.insert", map[string]any{"plugin": name, "collection": "received", "data": map[string]any{"hook": hook, "event": p["event"], "plugins": p["plugins"]}})
		}
		return map[string]any{"ok": true}, nil
	case "filter":
		if p["hook"] == "fail" {
			return nil, fmt.Errorf("filter failed")
		}
		if s, ok := p["value"].(string); ok {
			return s + "|sdk", nil
		}
		return p["value"], nil
	case "activate", "deactivate":
		name, _ := p["plugin"].(string)
		_, err := rt.call("ctx.store.insert", map[string]any{"plugin": name, "collection": "lifecycle", "data": map[string]any{"event": method}})
		return map[string]any{"ok": true}, err
	case "endpoint", "webhook":
		return rt.http(method, p)
	}
	return nil, fmt.Errorf("unknown method: %s", method)
}

func (rt *runtime) http(kind string, p map[string]any) (any, error) {
	name, _ := p["plugin"].(string)
	path, _ := p["path"].(string)
	switch {
	case strings.HasPrefix(path, "/echo"):
		return map[string]any{
			"kind": kind, "method": p["method"], "path": path, "params": p["params"], "query": p["query"],
			"body": p["body"], "headers": p["headers"], "httpContract": p["httpContract"],
			"rawBodyBase64": p["rawBodyBase64"], "clientIp": p["clientIp"],
		}, nil
	case path == "/boom":
		return nil, fmt.Errorf("secret failure detail")
	case path == "/envelope":
		return map[string]any{"__escalated_http": 1, "status": 201, "format": "json", "headers": map[string]any{"x-plugin": "yes"}, "body": map[string]any{"ok": true}}, nil
	case path == "/config":
		return rt.call("ctx.config.get", map[string]any{"plugin": name, "key": "api_key"})
	case path == "/ctx":
		body, _ := p["body"].(map[string]any)
		method, _ := body["method"].(string)
		params, _ := body["params"].(map[string]any)
		if params == nil {
			params = map[string]any{}
		}
		params["plugin"] = name
		v, err := rt.call(method, params)
		if err != nil {
			return map[string]any{"error": err.Error()}, nil
		}
		return map[string]any{"result": v}, nil
	case path == "/Settings":
		return map[string]any{"greeting": "hello from " + name}, nil
	case path == "/spy":
		query, _ := p["query"].(map[string]any)
		as, _ := query["as"].(string)
		v, err := rt.call("ctx.store.get", map[string]any{"plugin": as, "collection": "secrets", "key": "token"})
		if err != nil {
			return map[string]any{"error": err.Error()}, nil
		}
		return map[string]any{"value": v}, nil
	}
	return nil, fmt.Errorf("no handler for %s", path)
}
