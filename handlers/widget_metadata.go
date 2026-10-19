package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/escalated-dev/escalated-go/models"
)

// Limits on the host parameters (order number, plan, page URL, ...) an
// embedding page sends with a widget ticket or chat. They are visitor
// supplied, so only a flat map of short scalar values is kept.
const (
	widgetMetadataMaxKeys        = 50
	widgetMetadataMaxEntries     = 25
	widgetMetadataMaxValueLength = 500
	widgetMetadataMaxURLLength   = 2048
)

var widgetMetadataKeyRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

var errWidgetMetadata = errors.New("metadata must be an object with at most 50 keys")

type widgetMetadataEntry struct {
	key   string
	value json.RawMessage
}

// sanitizeWidgetMetadata cleans the optional "metadata" field of a widget
// request. It returns nil when nothing is kept, and an error only when the
// field is present and not an object or has more than 50 keys. embeddedOn is
// the allowed cross-origin Origin (WidgetCrossOrigin), recorded as
// "embedded_on" in place of anything the page sent under that key.
func sanitizeWidgetMetadata(raw json.RawMessage, embeddedOn string) (map[string]any, error) {
	entries, err := widgetMetadataEntries(raw)
	if err != nil {
		return nil, err
	}

	clean := map[string]any{}
	for _, e := range entries {
		if len(clean) >= widgetMetadataMaxEntries {
			break
		}
		if !widgetMetadataKeyRe.MatchString(e.key) {
			continue
		}
		if value, ok := widgetMetadataScalar(e.value); ok {
			clean[e.key] = value
		}
	}

	if pageURL, ok := clean["page_url"]; ok && !isWidgetWebURL(pageURL) {
		delete(clean, "page_url")
	}
	delete(clean, "embedded_on")
	if embeddedOn != "" {
		clean["embedded_on"] = embeddedOn
	}
	if len(clean) == 0 {
		return nil, nil
	}
	return clean, nil
}

// widgetMetadataEntries reads the object's members in the order they were
// sent, so the 25-entry cut keeps the first ones as other backends do.
func widgetMetadataEntries(raw json.RawMessage) ([]widgetMetadataEntry, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errWidgetMetadata
	}
	var entries []widgetMetadataEntry
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, errWidgetMetadata
		}
		key, _ := tok.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, errWidgetMetadata
		}
		entries = append(entries, widgetMetadataEntry{key: key, value: value})
		if len(entries) > widgetMetadataMaxKeys {
			return nil, errWidgetMetadata
		}
	}
	return entries, nil
}

// widgetMetadataScalar keeps strings (trimmed, cut to 500 characters),
// numbers and booleans; arrays, objects and null are dropped.
func widgetMetadataScalar(raw json.RawMessage) (any, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	switch value := v.(type) {
	case string:
		value = strings.TrimSpace(value)
		if utf8.RuneCountInString(value) > widgetMetadataMaxValueLength {
			value = string([]rune(value)[:widgetMetadataMaxValueLength])
		}
		return value, true
	case json.Number:
		if n, err := value.Int64(); err == nil {
			return n, true
		}
		if f, err := value.Float64(); err == nil {
			return f, true
		}
		return nil, false
	case bool:
		return value, true
	default:
		return nil, false
	}
}

func isWidgetWebURL(v any) bool {
	s, ok := v.(string)
	if !ok || s == "" || len(s) > widgetMetadataMaxURLLength {
		return false
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return scheme == "http" || scheme == "https"
}

// widgetMetadataJSON encodes sanitised metadata for a JSON column, or nil.
func widgetMetadataJSON(metadata map[string]any) models.JSONText {
	if len(metadata) == 0 {
		return nil
	}
	b, err := json.Marshal(metadata)
	if err != nil {
		return nil
	}
	return b
}
