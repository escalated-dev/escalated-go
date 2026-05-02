// Package i18n is a thin loader that fetches translation data from the central
// escalated-locale Go module and merges it with optional local override JSON
// files.
//
// Translation strings live upstream at github.com/escalated-dev/escalated-locale.
// This package adds two responsibilities on top of the upstream data:
//  1. layered overrides — JSON files at internal/i18n/overrides/{locale}.json
//     are deep-merged on top of the central data, so a host application can
//     tweak individual strings without forking the whole locale file;
//  2. a tiny T() helper — does dotted-key lookup and {param} interpolation.
//
// Pluralisation and gender selection are intentionally out of scope: the upstream
// package ships data only, and the per-host i18n stack (or a future helper) can
// layer pluralisation rules on top if the need arises.
package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	locale "github.com/escalated-dev/escalated-locale/packages/go"
)

// DefaultLocale is used as the fallback when a key is missing from the requested
// locale or when an unknown locale is requested.
const DefaultLocale = "en"

// overridesDir is the on-disk location where local override JSON files live.
// Keep it relative so consumers can drop their own overrides into the working
// directory at deploy time without rebuilding.
var overridesDir = filepath.Join("internal", "i18n", "overrides")

var (
	cacheMu sync.RWMutex
	cache   = map[string]map[string]any{}
)

// Load returns the merged translation map for the given locale.
//
// Precedence (lowest → highest):
//  1. Central data from github.com/escalated-dev/escalated-locale.
//  2. Local override file at internal/i18n/overrides/{locale}.json (optional).
//
// Results are cached per locale; call ResetCache to clear (mainly useful in
// tests or after writing a new override file at runtime).
func Load(loc string) map[string]any {
	if loc == "" {
		loc = DefaultLocale
	}

	cacheMu.RLock()
	if cached, ok := cache[loc]; ok {
		cacheMu.RUnlock()
		return cached
	}
	cacheMu.RUnlock()

	cacheMu.Lock()
	defer cacheMu.Unlock()
	// Re-check after acquiring the write lock.
	if cached, ok := cache[loc]; ok {
		return cached
	}

	merged := cloneMap(locale.GetLocaleData(loc))
	if merged == nil {
		merged = map[string]any{}
	}

	if overrides, err := loadOverride(loc); err == nil && overrides != nil {
		mergeMap(merged, overrides)
	}

	cache[loc] = merged
	return merged
}

// ResetCache clears the in-memory translation cache. Primarily intended for
// tests; production code can rely on lazy loading.
func ResetCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache = map[string]map[string]any{}
}

// T resolves a dotted key against the merged translations for the given locale,
// interpolates {param}-style placeholders, and falls back to DefaultLocale, then
// to the raw key, when the key is missing.
//
//	T("ticket.status.open", "fr", nil)            → "Ouvert"
//	T("validation.required", "en", map[string]any{"field": "Email"})
//	                                              → "Email is required"
func T(key, loc string, params map[string]any) string {
	if v, ok := lookup(Load(loc), key); ok {
		return interpolate(v, params)
	}
	if loc != DefaultLocale {
		if v, ok := lookup(Load(DefaultLocale), key); ok {
			return interpolate(v, params)
		}
	}
	return key
}

// lookup walks a dotted key against a nested map[string]any tree. It returns
// (value, true) only when the leaf is a string.
func lookup(data map[string]any, key string) (string, bool) {
	if data == nil || key == "" {
		return "", false
	}
	parts := strings.Split(key, ".")
	var current any = data
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		next, ok := m[part]
		if !ok {
			return "", false
		}
		current = next
	}
	s, ok := current.(string)
	return s, ok
}

// interpolate replaces {name} placeholders in s with the matching value from
// params. Missing params are left as the literal placeholder so the gap is
// obvious in output.
func interpolate(s string, params map[string]any) string {
	if len(params) == 0 || !strings.Contains(s, "{") {
		return s
	}
	for k, v := range params {
		s = strings.ReplaceAll(s, "{"+k+"}", fmt.Sprintf("%v", v))
	}
	return s
}

// loadOverride reads internal/i18n/overrides/{locale}.json. A missing file is
// not an error — overrides are optional.
func loadOverride(loc string) (map[string]any, error) {
	path := filepath.Join(overridesDir, loc+".json")
	raw, err := os.ReadFile(path) //nolint:gosec // path is built from a known dir + locale code
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("i18n: parse override %s: %w", path, err)
	}
	return out, nil
}

// mergeMap deep-merges src into dst. Nested maps recurse; scalar values in src
// overwrite whatever is in dst.
func mergeMap(dst, src map[string]any) {
	for k, sv := range src {
		if dv, ok := dst[k]; ok {
			dm, dok := dv.(map[string]any)
			sm, sok := sv.(map[string]any)
			if dok && sok {
				mergeMap(dm, sm)
				continue
			}
		}
		dst[k] = sv
	}
}

// cloneMap returns a deep copy of m so callers can mutate it (e.g. when merging
// overrides) without scribbling on the central package's cached data.
func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if nested, ok := v.(map[string]any); ok {
			out[k] = cloneMap(nested)
		} else {
			out[k] = v
		}
	}
	return out
}
