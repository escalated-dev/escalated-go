package models

import "sort"

// LegacyEventNames maps each legacy domain-event name to its canonical name
// (escalated-developer-context decisions/2026-10-21-event-vocabulary.md).
//
// Escalated emits and offers only canonical names. Legacy names are still
// accepted wherever stored data is read (a workflow's trigger_event, a
// webhook's subscribed events) and are treated as their canonical name.
var LegacyEventNames = map[string]string{
	"ticket.replied":        "reply.created",
	"internal_note.added":   "note.created",
	"internal_note.created": "note.created",
	"tag.added":             "ticket.tag_added",
	"ticket.tagged":         "ticket.tag_added",
	"tag.removed":           "ticket.tag_removed",
	"ticket.untagged":       "ticket.tag_removed",
	"chat.session_started":  "chat.started",
	"chat.session_ended":    "chat.ended",
}

// CanonicalEventName returns the canonical name for an event name, which is
// the name itself unless it is a legacy alias.
func CanonicalEventName(name string) string {
	if canonical, ok := LegacyEventNames[name]; ok {
		return canonical
	}
	return name
}

// EventNamesFor returns the canonical name followed by every legacy name that
// means it, for matching stored rows that may still hold a legacy name.
func EventNamesFor(event string) []string {
	canonical := CanonicalEventName(event)
	names := []string{canonical}
	var legacy []string
	for old, c := range LegacyEventNames {
		if c == canonical {
			legacy = append(legacy, old)
		}
	}
	sort.Strings(legacy)
	return append(names, legacy...)
}

// CanonicalEventNames maps a list of event names to canonical names, dropping
// duplicates and keeping the first-seen order.
func CanonicalEventNames(names []string) []string {
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, n := range names {
		c := CanonicalEventName(n)
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}
