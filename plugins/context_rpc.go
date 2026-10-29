package plugins

import (
	"context"
	"fmt"
	"math"
	"strconv"
)

// serveContext answers one ctx.* call from the runtime for plugin, which the
// bridge has already checked is one it is serving. A plugin parameter naming
// any other plugin is never used.
func (h *Host) serveContext(ctx context.Context, plugin, method string, params map[string]any) (any, error) {
	var e *entry
	if h.bridge != nil {
		for _, candidate := range h.bridge.known() {
			if candidate.name == plugin {
				e = candidate
				break
			}
		}
	}
	if e == nil {
		return nil, fmt.Errorf("unknown plugin %q", plugin)
	}
	c := h.context(e, requestFrom(ctx))
	if fn, ok := contextMethods[method]; ok {
		return fn(ctx, c, params)
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

type contextMethod func(ctx context.Context, c *Context, params map[string]any) (any, error)

var contextMethods = map[string]contextMethod{
	"ctx.config.all": func(ctx context.Context, c *Context, _ map[string]any) (any, error) {
		return c.Config.All(ctx)
	},
	"ctx.config.get": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return c.Config.Get(ctx, paramString(p, "key"))
	},
	"ctx.config.set": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		data := asRecord(p["data"])
		if data == nil {
			return nil, fmt.Errorf("ctx.config.set requires an object")
		}
		return nil, c.Config.Set(ctx, data)
	},
	"ctx.store.get": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return c.Store.Get(ctx, paramString(p, "collection"), paramString(p, "key"))
	},
	"ctx.store.set": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return nil, c.Store.Set(ctx, paramString(p, "collection"), paramString(p, "key"), p["value"])
	},
	"ctx.store.query": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		var opts QueryOptions
		if o := asRecord(p["options"]); o != nil {
			if l, ok := o["limit"].(float64); ok {
				n := int(l)
				opts.Limit = &n
			}
			opts.OrderBy = paramString(o, "orderBy")
			opts.Order = paramString(o, "order")
		}
		filter := asRecord(p["filter"])
		if filter == nil {
			filter = map[string]any{}
		}
		return c.Store.Query(ctx, paramString(p, "collection"), filter, opts)
	},
	"ctx.store.insert": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return c.Store.Insert(ctx, paramString(p, "collection"), asRecord(p["data"]))
	},
	"ctx.store.update": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return c.Store.Update(ctx, paramString(p, "collection"), paramString(p, "key"), asRecord(p["data"]))
	},
	"ctx.store.delete": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return nil, c.Store.Delete(ctx, paramString(p, "collection"), paramString(p, "key"))
	},
	"ctx.log": func(_ context.Context, c *Context, p map[string]any) (any, error) {
		message := paramString(p, "message")
		var data []any
		if v, ok := p["data"]; ok && v != nil {
			data = []any{v}
		}
		switch paramString(p, "level") {
		case "warn":
			c.Log.Warn(message, data...)
		case "error":
			c.Log.Error(message, data...)
		case "debug":
			c.Log.Debug(message, data...)
		default:
			c.Log.Info(message, data...)
		}
		return nil, nil
	},
	"ctx.emit": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return nil, c.Emit(ctx, paramString(p, "hook"), p["data"])
	},
	"ctx.tickets.find": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return nilIfEmpty(c.Tickets().Find(ctx, p["id"]))
	},
	"ctx.tickets.query": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return c.Tickets().Query(ctx, recordParam(p, "filter"))
	},
	"ctx.tickets.create": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return c.Tickets().Create(ctx, asRecord(p["data"]))
	},
	"ctx.tickets.update": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return c.Tickets().Update(ctx, p["id"], asRecord(p["data"]))
	},
	"ctx.replies.find": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return nilIfEmpty(c.Replies().Find(ctx, p["id"]))
	},
	"ctx.replies.query": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return c.Replies().Query(ctx, recordParam(p, "filter"))
	},
	"ctx.replies.create": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return c.Replies().Create(ctx, asRecord(p["data"]))
	},
	"ctx.contacts.find": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return nilIfEmpty(c.Contacts().Find(ctx, p["id"]))
	},
	"ctx.contacts.findByEmail": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return nilIfEmpty(c.Contacts().FindByEmail(ctx, paramString(p, "email")))
	},
	"ctx.contacts.create": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return c.Contacts().Create(ctx, recordParam(p, "data"))
	},
	"ctx.tags.all": func(ctx context.Context, c *Context, _ map[string]any) (any, error) {
		return c.Tags().All(ctx)
	},
	"ctx.tags.create": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return c.Tags().Create(ctx, recordParam(p, "data"))
	},
	"ctx.departments.all": func(ctx context.Context, c *Context, _ map[string]any) (any, error) {
		return c.Departments().All(ctx)
	},
	"ctx.departments.find": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return nilIfEmpty(c.Departments().Find(ctx, p["id"]))
	},
	"ctx.agents.all": func(ctx context.Context, c *Context, _ map[string]any) (any, error) {
		return c.Agents().All(ctx)
	},
	"ctx.agents.find": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		agent, err := c.Agents().Find(ctx, p["id"])
		if agent == nil || err != nil {
			return nil, err
		}
		return agent, nil
	},
	"ctx.broadcast.toChannel": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return nil, c.Broadcast().ToChannel(ctx, paramString(p, "channel"), paramString(p, "event"), p["data"])
	},
	"ctx.broadcast.toUser": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return nil, c.Broadcast().ToUser(ctx, p["userId"], paramString(p, "event"), p["data"])
	},
	"ctx.broadcast.toTicket": func(ctx context.Context, c *Context, p map[string]any) (any, error) {
		return nil, c.Broadcast().ToTicket(ctx, p["ticketId"], paramString(p, "event"), p["data"])
	},
	"ctx.currentUser": func(ctx context.Context, c *Context, _ map[string]any) (any, error) {
		u, err := c.CurrentUser(ctx)
		if u == nil || err != nil {
			return nil, err
		}
		return u, nil
	},
}

// nilIfEmpty answers JSON null for a record that was not found.
func nilIfEmpty(m map[string]any, err error) (any, error) {
	if m == nil || err != nil {
		return nil, err
	}
	return m, nil
}

func recordParam(p map[string]any, key string) map[string]any {
	if m := asRecord(p[key]); m != nil {
		return m
	}
	return map[string]any{}
}

// paramString reads a string parameter; a whole number is written without a
// decimal point.
func paramString(p map[string]any, key string) string {
	switch v := p[key].(type) {
	case string:
		return v
	case float64:
		if v == math.Trunc(v) {
			return strconv.FormatFloat(v, 'f', 0, 64)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}
