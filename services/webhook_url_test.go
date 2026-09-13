package services

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

func TestWebhookAddrAllowed(t *testing.T) {
	for addr, want := range map[string]bool{
		"93.184.215.14":        true,
		"8.8.8.8":              true,
		"2606:4700::6810:84e5": true,
		"127.0.0.1":            false,
		"127.8.8.8":            false,
		"::1":                  false,
		"10.0.0.1":             false,
		"172.16.0.1":           false,
		"172.31.255.255":       false,
		"192.168.0.1":          false,
		"169.254.169.254":      false,
		"fe80::1":              false,
		"fd00:ec2::254":        false,
		"0.0.0.0":              false,
		"::":                   false,
		"100.64.0.1":           false,
		"100.100.100.200":      false,
		"224.0.0.1":            false,
		"255.255.255.255":      false,
		"::ffff:127.0.0.1":     false,
		"::ffff:10.1.2.3":      false,
		"::ffff:93.184.215.14": true,
	} {
		if got := webhookAddrAllowed(netip.MustParseAddr(addr)); got != want {
			t.Errorf("webhookAddrAllowed(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestValidateWebhookURLChecksWhatAHostnameResolvesTo(t *testing.T) {
	previous := lookupWebhookHost
	t.Cleanup(func() { lookupWebhookHost = previous })

	lookupWebhookHost = func(_ context.Context, host string) ([]netip.Addr, error) {
		switch host {
		case "hooks.example":
			return []netip.Addr{netip.MustParseAddr("93.184.215.14")}, nil
		case "rebound.example":
			// One public and one private answer: any private answer is enough.
			return []netip.Addr{netip.MustParseAddr("93.184.215.14"), netip.MustParseAddr("10.0.0.7")}, nil
		default:
			return nil, errors.New("no such host")
		}
	}

	ctx := context.Background()
	if err := ValidateWebhookURL(ctx, "https://hooks.example/in"); err != nil {
		t.Errorf("public hostname refused: %v", err)
	}
	if err := ValidateWebhookURL(ctx, "https://rebound.example/in"); !errors.Is(err, ErrWebhookDestinationNotAllowed) {
		t.Errorf("hostname with a private answer: got %v, want ErrWebhookDestinationNotAllowed", err)
	}
	// Unresolvable today is accepted; the dial-time check covers it later.
	if err := ValidateWebhookURL(ctx, "https://not-yet.example/in"); err != nil {
		t.Errorf("unresolvable hostname refused: %v", err)
	}
}
