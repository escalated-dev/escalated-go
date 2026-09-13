package handlers

import "crypto/subtle"

// guestTokenMatches reports whether given is the guest token stored on a
// ticket. The comparison takes the same time wherever the two tokens differ,
// so a caller probing one reference learns nothing about the stored token from
// response timing. An empty token never matches.
func guestTokenMatches(stored *string, given string) bool {
	if stored == nil || *stored == "" || given == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(*stored), []byte(given)) == 1
}
