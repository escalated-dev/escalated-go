package models

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// referenceShape is a prefix, the year and month, and 8 characters of
// Crockford base32: digits and uppercase letters without I, L, O and U.
var referenceShape = regexp.MustCompile(`^([A-Z]+)-(\d{4})-([0-9A-HJKMNP-TV-Z]{8})$`)

func TestGenerateReferenceFormat(t *testing.T) {
	tests := []struct {
		prefix     string
		wantPrefix string
	}{
		{prefix: "", wantPrefix: "ESC"},
		{prefix: "ESC", wantPrefix: "ESC"},
		{prefix: "HELP", wantPrefix: "HELP"},
	}

	for _, tt := range tests {
		before := time.Now().Format("0601")
		ref := GenerateReference(tt.prefix)
		after := time.Now().Format("0601")

		m := referenceShape.FindStringSubmatch(ref)
		if m == nil {
			t.Fatalf("GenerateReference(%q) = %q, want PREFIX-YYMM-XXXXXXXX with 8 Crockford base32 characters", tt.prefix, ref)
		}
		if m[1] != tt.wantPrefix {
			t.Errorf("GenerateReference(%q) prefix = %q, want %q", tt.prefix, m[1], tt.wantPrefix)
		}
		if m[2] != before && m[2] != after {
			t.Errorf("GenerateReference(%q) date part = %q, want %q", tt.prefix, m[2], before)
		}
	}
}

// The inbound email parser finds a reference in a subject with
// `\[([A-Z]+-[0-9A-Z-]+)\]`, so every character has to stay inside that class.
func TestGenerateReferenceMatchesTheInboundSubjectTag(t *testing.T) {
	subjectTag := regexp.MustCompile(`\[([A-Z]+-[0-9A-Z-]+)\]`)

	for i := 0; i < 200; i++ {
		ref := GenerateReference("")
		m := subjectTag.FindStringSubmatch("Re: [" + ref + "] Printer on fire")
		if m == nil || m[1] != ref {
			t.Fatalf("subject tag pattern did not capture %q (got %v)", ref, m)
		}
	}
}

func TestFormatReferenceEncodesFortyBits(t *testing.T) {
	now := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)

	vectors := []struct {
		random [5]byte
		want   string
	}{
		{random: [5]byte{0x00, 0x00, 0x00, 0x00, 0x00}, want: "ESC-2609-00000000"},
		{random: [5]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, want: "ESC-2609-ZZZZZZZZ"},
		// 00000 00001 00010 00011 00100 00101 00110 00111
		{random: [5]byte{0x00, 0x44, 0x32, 0x14, 0xC7}, want: "ESC-2609-01234567"},
		// 01000 01001 01010 01011 01100 01101 01110 01111: 8, 9, A, B, C, D, E, F
		{random: [5]byte{0x42, 0x54, 0xB6, 0x35, 0xCF}, want: "ESC-2609-89ABCDEF"},
		// 10000 ... 10111: 16-23 are G H J K M N P Q (no I, no L)
		{random: [5]byte{0x84, 0x65, 0x3A, 0x56, 0xD7}, want: "ESC-2609-GHJKMNPQ"},
		// 11000 ... 11111: 24-31 are R S T V W X Y Z (no U)
		{random: [5]byte{0xC6, 0x75, 0xBE, 0x77, 0xDF}, want: "ESC-2609-RSTVWXYZ"},
	}

	for _, v := range vectors {
		if got := formatReference("ESC", now, v.random); got != v.want {
			t.Errorf("formatReference(% X) = %q, want %q", v.random, got, v.want)
		}
	}

	// Every one of the 40 random bits reaches the suffix: flipping any single
	// bit gives a reference no other flip gives.
	var base [5]byte
	seen := map[string]int{formatReference("ESC", now, base): -1}
	for bit := 0; bit < 40; bit++ {
		flipped := base
		flipped[bit/8] ^= 0x80 >> (bit % 8)
		ref := formatReference("ESC", now, flipped)
		if prev, dup := seen[ref]; dup {
			t.Fatalf("flipping bit %d gives %q, the same as bit %d: that bit is lost", bit, ref, prev)
		}
		seen[ref] = bit
	}

	if suffix := strings.TrimPrefix(formatReference("ESC", now, base), "ESC-2609-"); len(suffix) != 8 {
		t.Fatalf("suffix %q has %d characters, want 8", suffix, len(suffix))
	}
}
