package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ErrWebhookDestinationNotAllowed is returned for a webhook URL, or a
// connection, that would reach an address outbound webhooks must not: loopback,
// private networks, link-local ranges (the cloud metadata endpoint among them)
// and other addresses that are not publicly routable.
var ErrWebhookDestinationNotAllowed = errors.New("webhook destination is not allowed")

// lookupWebhookHost resolves a webhook hostname. It is a variable so tests can
// answer without DNS.
var lookupWebhookHost = func(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// nonPublicPrefixes are ranges netip's predicates do not already cover that are
// still not publicly routable.
var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),     // "this network"
	netip.MustParsePrefix("100.64.0.0/10"), // shared address space (carrier-grade NAT)
	netip.MustParsePrefix("192.0.0.0/24"),  // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),   // reserved, including broadcast
}

// webhookAddrAllowed reports whether an outbound webhook may connect to addr.
func webhookAddrAllowed(addr netip.Addr) bool {
	// An IPv4-mapped IPv6 address (::ffff:127.0.0.1) is the IPv4 address.
	addr = addr.Unmap()
	if !addr.IsValid() ||
		addr.IsUnspecified() ||
		addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() ||
		addr.IsMulticast() {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// ValidateWebhookURL checks a webhook URL before it is stored. It must be http
// or https with a host, and that host must not be, or resolve to, an address
// webhookAddrAllowed refuses.
//
// A hostname that does not resolve right now is accepted. That is not the last
// check: the dispatcher's default client checks the address it actually
// connects to on every delivery, so a name that later resolves somewhere
// private is refused then.
func ValidateWebhookURL(ctx context.Context, raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWebhookDestinationNotAllowed, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: the url must use http or https", ErrWebhookDestinationNotAllowed)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: the url has no host", ErrWebhookDestinationNotAllowed)
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		if !webhookAddrAllowed(addr) {
			return fmt.Errorf("%w: %s is not a public address", ErrWebhookDestinationNotAllowed, addr)
		}
		return nil
	}

	addrs, err := lookupWebhookHost(ctx, host)
	if err != nil {
		return nil
	}
	for _, addr := range addrs {
		if !webhookAddrAllowed(addr) {
			return fmt.Errorf("%w: %s resolves to %s, which is not a public address", ErrWebhookDestinationNotAllowed, host, addr)
		}
	}
	return nil
}

// newWebhookHTTPClient builds the dispatcher's default client.
//
// Every connection is checked against webhookAddrAllowed on the address being
// dialled, after DNS resolution, so a hostname that resolves somewhere private
// is refused even if it pointed somewhere public when it was saved.
//
// Redirects are not followed. The 3xx is recorded as the response, because
// following it would let the receiver pick the destination.
//
// No proxy is used, because a proxy would connect on the dispatcher's behalf to
// an address this check never sees. A host that needs one should set
// WebhookDispatcher.Client, and that client then owns the policy.
func newWebhookHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		Control:   refuseNonPublicWebhookAddr,
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialer.DialContext

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// refuseNonPublicWebhookAddr is a net.Dialer Control hook. address is the
// resolved ip:port about to be connected.
func refuseNonPublicWebhookAddr(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	if !webhookAddrAllowed(addr) {
		return fmt.Errorf("%w: %s is not a public address", ErrWebhookDestinationNotAllowed, addr)
	}
	return nil
}
