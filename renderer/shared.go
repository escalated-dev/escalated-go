package renderer

import "context"

type sharedKey struct{}

type sharedProp struct {
	key   string
	value func() any
}

// WithShared adds a prop that every page rendered for the request carries,
// computed when the page is rendered. A page's own prop of the same name
// wins; when both are objects they are merged, the page's keys first, and
// so on for objects nested in both. A prop shared earlier (by middleware
// that runs first, such as the host's) wins over one shared later the same
// way.
func WithShared(ctx context.Context, key string, value func() any) context.Context {
	existing, _ := ctx.Value(sharedKey{}).([]sharedProp)
	next := append(append([]sharedProp{}, existing...), sharedProp{key: key, value: value})
	return context.WithValue(ctx, sharedKey{}, next)
}

// withSharedProps returns props with the request's shared props added.
func withSharedProps(ctx context.Context, props map[string]any) map[string]any {
	shared, _ := ctx.Value(sharedKey{}).([]sharedProp)
	if len(shared) == 0 {
		return props
	}
	out := make(map[string]any, len(props)+len(shared))
	for k, v := range props {
		out[k] = v
	}
	for _, s := range shared {
		value := s.value()
		current, exists := out[s.key]
		if !exists {
			out[s.key] = value
			continue
		}
		own, ownIsMap := current.(map[string]any)
		extra, extraIsMap := value.(map[string]any)
		if !ownIsMap || !extraIsMap {
			continue
		}
		out[s.key] = mergeOwnFirst(own, extra)
	}
	return out
}

// mergeOwnFirst merges extra into own: own's keys win, and where both hold an
// object the two are merged the same way.
func mergeOwnFirst(own, extra map[string]any) map[string]any {
	merged := make(map[string]any, len(own)+len(extra))
	for k, v := range extra {
		merged[k] = v
	}
	for k, v := range own {
		ownMap, ownIsMap := v.(map[string]any)
		extraMap, extraIsMap := merged[k].(map[string]any)
		if ownIsMap && extraIsMap {
			merged[k] = mergeOwnFirst(ownMap, extraMap)
			continue
		}
		merged[k] = v
	}
	return merged
}
