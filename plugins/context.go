package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Context is a plugin's ctx: everything in it is scoped to the plugin it was
// built for. Native plugins receive it in every callback; for SDK plugins the
// bridge answers the runtime's ctx.* calls through the same methods.
type Context struct {
	// Plugin is the plugin's name, which its store and config are keyed by.
	Plugin string
	// Slug is the plugin's slug.
	Slug string

	Config ConfigAPI
	Store  StoreAPI
	Log    LogAPI

	host    *Host
	request *http.Request
}

func (h *Host) context(e *entry, r *http.Request) *Context {
	c := &Context{Plugin: e.name, Slug: e.slug, host: h, request: r}
	c.Config = ConfigAPI{c: c}
	c.Store = StoreAPI{c: c}
	c.Log = LogAPI{c: c}
	return c
}

// Request is the HTTP request the plugin is serving, or nil outside one (an
// action fired by a background job, a lifecycle callback with no request).
func (c *Context) Request() *http.Request { return c.request }

// Emit fires an action of the plugin's choosing to every active plugin. Names
// that belong to the host (the canonical domain events, their legacy names and
// the plugin.* lifecycle actions) are dropped and logged.
func (c *Context) Emit(ctx context.Context, hook string, data any) error {
	if hook == "" || c.host.reserved[hook] {
		c.Log.Warn(fmt.Sprintf("ctx.emit(%q) dropped: that name belongs to the host", hook))
		return nil
	}
	clean, err := roundTrip(data)
	if err != nil {
		return err
	}
	event := asRecord(clean)
	if event == nil {
		event = map[string]any{"value": clean}
	}
	c.host.DispatchAction(withRequest(ctx, c.request), hook, event)
	return nil
}

// User is ctx.currentUser's answer.
type User struct {
	ID    any     `json:"id"`
	Name  *string `json:"name"`
	Email *string `json:"email"`
}

// CurrentUser is the signed-in user of the request being served, or nil.
func (c *Context) CurrentUser(ctx context.Context) (*User, error) {
	if c.request == nil {
		return nil, nil
	}
	id := c.host.userID(c.request)
	if id.Empty() {
		return nil, nil
	}
	u := &User{ID: id}
	if c.host.userName != nil {
		if name := c.host.userName(ctx, id); name != "" {
			u.Name = &name
		}
	}
	if c.host.userEmail != nil {
		if email, err := c.host.userEmail(ctx, id); err == nil && email != "" {
			u.Email = &email
		}
	}
	return u, nil
}

// ConfigAPI is ctx.config: the plugin's settings, one JSON object.
type ConfigAPI struct{ c *Context }

// All returns every setting.
func (a ConfigAPI) All(ctx context.Context) (map[string]any, error) {
	return a.c.host.rows.configAll(ctx, a.c.Plugin)
}

// Get returns one setting, or nil.
func (a ConfigAPI) Get(ctx context.Context, key string) (any, error) {
	return a.c.host.rows.configGet(ctx, a.c.Plugin, key)
}

// Set merges data into the settings.
func (a ConfigAPI) Set(ctx context.Context, data map[string]any) error {
	return a.c.host.rows.configSet(ctx, a.c.Plugin, data)
}

// StoreAPI is ctx.store: the plugin's records, by collection and key.
type StoreAPI struct{ c *Context }

// Get returns the value stored under key, or nil.
func (a StoreAPI) Get(ctx context.Context, collection, key string) (any, error) {
	return a.c.host.rows.get(ctx, a.c.Plugin, collection, key)
}

// Set stores value under key, replacing what was there.
func (a StoreAPI) Set(ctx context.Context, collection, key string, value any) error {
	clean, err := roundTrip(value)
	if err != nil {
		return err
	}
	return a.c.host.rows.set(ctx, a.c.Plugin, collection, key, clean)
}

// Query returns the records matching filter: equality (an array field matches
// when it contains the value) and $gt $gte $lt $lte $ne $in $nin, on dotted
// field names. Each record is its data with the row id as _id.
func (a StoreAPI) Query(ctx context.Context, collection string, filter map[string]any, opts QueryOptions) ([]StoreRecord, error) {
	return a.c.host.rows.query(ctx, a.c.Plugin, collection, filter, opts)
}

// Insert adds a record; its key is data["key"], when given.
func (a StoreAPI) Insert(ctx context.Context, collection string, data map[string]any) (StoreRecord, error) {
	return a.c.host.rows.insert(ctx, a.c.Plugin, collection, data)
}

// Update merges data into the record with key.
func (a StoreAPI) Update(ctx context.Context, collection, key string, data map[string]any) (StoreRecord, error) {
	return a.c.host.rows.update(ctx, a.c.Plugin, collection, key, data)
}

// Delete removes the records with key.
func (a StoreAPI) Delete(ctx context.Context, collection, key string) error {
	return a.c.host.rows.delete(ctx, a.c.Plugin, collection, key)
}

// LogAPI is ctx.log: the host log, with the plugin's name attached.
type LogAPI struct{ c *Context }

func (a LogAPI) write(level, message string, data []any) {
	line := fmt.Sprintf("[%s] %s: %s", a.c.Plugin, level, message)
	if len(data) > 0 {
		var v any = data
		if len(data) == 1 {
			v = data[0]
		}
		if b, err := json.Marshal(v); err == nil {
			line += " " + string(b)
		}
	}
	a.c.host.logf("%s", line)
}

// Info logs at info level.
func (a LogAPI) Info(message string, data ...any) { a.write("info", message, data) }

// Warn logs at warn level.
func (a LogAPI) Warn(message string, data ...any) { a.write("warn", message, data) }

// Error logs at error level.
func (a LogAPI) Error(message string, data ...any) { a.write("error", message, data) }

// Debug logs at debug level.
func (a LogAPI) Debug(message string, data ...any) { a.write("debug", message, data) }
