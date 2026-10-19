package handlers

import (
	"context"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestNormalizeWidgetOriginPattern(t *testing.T) {
	cases := []struct {
		entry string
		want  string
	}{
		{"https://Shop.Example.com", "https://shop.example.com"},
		{"https://shop.example.com/", "https://shop.example.com"},
		{"https://shop.example.com:443", "https://shop.example.com"},
		{"http://localhost:5173", "http://localhost:5173"},
		{"https://*.example.com", "https://*.example.com"},
		{"https://*.example.com:8443", "https://*.example.com:8443"},
		{"https://shop.example.com/checkout", ""},
		{"https://shop.example.com?x=1", ""},
		{"shop.example.com", ""},
		{"*", ""},
		{"https://*", ""},
		{"https://a.*.example.com", ""},
		{"ftp://shop.example.com", ""},
		{"https://user@shop.example.com", ""},
	}
	for _, tc := range cases {
		t.Run(tc.entry, func(t *testing.T) {
			if got := NormalizeWidgetOriginPattern(tc.entry); got != tc.want {
				t.Errorf("NormalizeWidgetOriginPattern(%q) = %q, want %q", tc.entry, got, tc.want)
			}
		})
	}
}

func TestWidgetOriginMatching(t *testing.T) {
	cfg := DefaultWidgetConfig()
	cfg.AllowedOrigins = "https://shop.example.com, https://*.brand.test,http://localhost:5173"
	policy := NewWidgetOriginPolicy(cfg, newWidgetMockStore())

	cases := []struct {
		origin string
		want   bool
	}{
		{"https://shop.example.com", true},
		{"https://SHOP.example.com:443", true},
		{"http://shop.example.com", false},
		{"https://shop.example.com:8443", false},
		{"https://help.brand.test", true},
		{"https://a.b.brand.test", true},
		{"https://brand.test", false},
		{"https://notbrand.test", false},
		{"https://help.brand.test.evil.com", false},
		{"http://localhost:5173", true},
		{"http://localhost", false},
		{"null", false},
	}
	for _, tc := range cases {
		t.Run(tc.origin, func(t *testing.T) {
			if got := policy.Allows(context.Background(), tc.origin); got != tc.want {
				t.Errorf("Allows(%q) = %v, want %v", tc.origin, got, tc.want)
			}
		})
	}
}

func TestWidgetOriginsMergeConfigWithSetting(t *testing.T) {
	ms := newWidgetMockStore()
	ms.settings[WidgetAllowedOriginsSetting] = "https://blog.example.com, https://bad.example.com/path\n"
	cfg := DefaultWidgetConfig()
	cfg.AllowedOrigins = "https://shop.example.com"
	policy := NewWidgetOriginPolicy(cfg, ms)

	got := policy.Patterns(context.Background())
	want := []string{"https://shop.example.com", "https://blog.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Patterns = %v, want %v", got, want)
	}
	if policy.Allows(context.Background(), "https://bad.example.com") {
		t.Error("an entry with a path must not allow its origin")
	}
}

func TestWidgetOriginsRejectBareStar(t *testing.T) {
	cfg := DefaultWidgetConfig()
	cfg.AllowedOrigins = "*"
	policy := NewWidgetOriginPolicy(cfg, newWidgetMockStore())
	if policy.Allows(context.Background(), "https://anything.example") {
		t.Error("a bare * must not allow every origin")
	}
}

func TestParseWidgetOrigins(t *testing.T) {
	patterns, invalid := ParseWidgetOrigins("https://shop.example.com\nhttps://*.brand.test, *  https://x.test/path")
	if want := []string{"https://shop.example.com", "https://*.brand.test"}; !reflect.DeepEqual(patterns, want) {
		t.Errorf("patterns = %v, want %v", patterns, want)
	}
	if want := []string{"*", "https://x.test/path"}; !reflect.DeepEqual(invalid, want) {
		t.Errorf("invalid = %v, want %v", invalid, want)
	}
}

func TestWidgetRequestCrossOrigin(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		want   string
	}{
		{"no Origin header", "", ""},
		{"null origin", "null", ""},
		{"same origin", "https://help.example.com", ""},
		{"same origin with default port", "https://help.example.com:443", ""},
		{"another site", "https://shop.example.com", "https://shop.example.com"},
		{"another scheme", "http://help.example.com", "http://help.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "https://help.example.com/support/widget/config", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if got := widgetRequestCrossOrigin(req); got != tc.want {
				t.Errorf("widgetRequestCrossOrigin = %q, want %q", got, tc.want)
			}
		})
	}
}
