package invocation

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// HTTPResolver reconstructs the canonical HTTP executor from a Descriptor.
type HTTPResolver struct{}

// Resolve implements Resolver. Network policy is enforced for every dial and
// redirect, not only when the descriptor is admitted.
func (HTTPResolver) Resolve(_ context.Context, descriptor Descriptor) (Executor, io.Closer, error) {
	if descriptor.Type() != DescriptorHTTP {
		return nil, nil, fmt.Errorf("HTTP resolver cannot resolve descriptor type %q", descriptor.Type())
	}
	settings := descriptor.Settings()
	method := strings.TrimSpace(settings["method"])
	if method == "" {
		method = http.MethodPost
	}
	timeout := 30 * time.Second
	if raw := strings.TrimSpace(settings["timeout"]); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return nil, nil, fmt.Errorf("invocation HTTP timeout must be a positive duration")
		}
		timeout = parsed
	}
	maxResponseBytes := int64(1 << 20)
	if raw := strings.TrimSpace(settings["max_response_bytes"]); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			return nil, nil, fmt.Errorf("invocation HTTP max_response_bytes must be positive")
		}
		maxResponseBytes = parsed
	}
	allowPrivate := false
	if raw := strings.TrimSpace(settings["allow_private_network"]); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("invocation HTTP allow_private_network must be boolean")
		}
		allowPrivate = parsed
	}
	policy := httpNetworkPolicy{allowPrivate: allowPrivate}
	if _, err := policy.validateURL(descriptor.Reference()); err != nil {
		return nil, nil, err
	}
	client, transport := policy.client(timeout, descriptor.Headers())
	executor, err := NewHTTPExecutor(HTTPExecutor{
		URL: descriptor.Reference(), Method: method, Headers: descriptor.Headers(),
		Client: client, MaxResponseBytes: maxResponseBytes,
	})
	if err != nil {
		transport.CloseIdleConnections()
		return nil, nil, err
	}
	return &describedHTTPExecutor{HTTPExecutor: executor, descriptor: descriptor}, httpTransportCloser{transport}, nil
}

type httpTransportCloser struct{ transport *http.Transport }

func (closer httpTransportCloser) Close() error {
	closer.transport.CloseIdleConnections()
	return nil
}

type describedHTTPExecutor struct {
	*HTTPExecutor
	descriptor Descriptor
}

func (executor *describedHTTPExecutor) InvocationResolverDescriptor() (Descriptor, error) {
	return executor.descriptor, nil
}

type httpNetworkPolicy struct{ allowPrivate bool }

func (policy httpNetworkPolicy) validateURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse invocation HTTP URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("invocation HTTP URL scheme must be http or https")
	}
	if parsed.Hostname() == "" || parsed.User != nil {
		return nil, fmt.Errorf("invocation HTTP URL must have a host and no user information")
	}
	if strings.EqualFold(parsed.Hostname(), "localhost") && !policy.allowPrivate {
		return nil, fmt.Errorf("invocation HTTP URL host is not allowed")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && !policy.allowedIP(ip) {
		return nil, fmt.Errorf("invocation HTTP URL IP is not allowed")
	}
	return parsed, nil
}

func (policy httpNetworkPolicy) client(timeout time.Duration, sensitiveHeaders map[string]string) (*http.Client, *http.Transport) {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{ForceAttemptHTTP2: true, IdleConnTimeout: 90 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve invocation HTTP host: %w", err)
		}
		if err := policy.validateResolvedIPs(addresses); err != nil {
			return nil, err
		}
		var last error
		for _, candidate := range addresses {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			last = dialErr
		}
		return nil, last
	}}
	return &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("invocation HTTP redirect limit exceeded")
		}
		if _, err := policy.validateURL(request.URL.String()); err != nil {
			return err
		}
		if len(via) > 0 && strings.EqualFold(via[len(via)-1].URL.Scheme, "https") && strings.EqualFold(request.URL.Scheme, "http") {
			return fmt.Errorf("invocation HTTPS redirect cannot downgrade to HTTP")
		}
		if len(via) > 0 && !sameHTTPOrigin(via[0].URL, request.URL) {
			request.Header.Del("Authorization")
			request.Header.Del("Cookie")
			for header := range sensitiveHeaders {
				request.Header.Del(header)
			}
			for header := range request.Header {
				if strings.HasPrefix(strings.ToLower(header), "x-effectus-") {
					request.Header.Del(header)
				}
			}
		}
		return nil
	}}, transport
}

func (policy httpNetworkPolicy) validateResolvedIPs(addresses []net.IPAddr) error {
	if len(addresses) == 0 {
		return fmt.Errorf("invocation HTTP host has no addresses")
	}
	for _, candidate := range addresses {
		if !policy.allowedIP(candidate.IP) {
			return fmt.Errorf("invocation HTTP host resolved to a disallowed address")
		}
	}
	return nil
}

func sameHTTPOrigin(left, right *url.URL) bool {
	if left == nil || right == nil || !strings.EqualFold(left.Scheme, right.Scheme) || !strings.EqualFold(left.Hostname(), right.Hostname()) {
		return false
	}
	port := func(value *url.URL) string {
		if value.Port() != "" {
			return value.Port()
		}
		if strings.EqualFold(value.Scheme, "https") {
			return "443"
		}
		return "80"
	}
	return port(left) == port(right)
}

// The default policy follows the IANA IPv4 and IPv6 Special-Purpose Address
// Registries' Globally Reachable status. Broad protocol-assignment blocks
// need the more-specific exceptions below; transition addresses that can
// embed private IPv4 destinations remain denied.
var httpNonPublicIPv4 = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

var httpPublicSpecialIPv4 = []netip.Prefix{
	netip.MustParsePrefix("192.0.0.9/32"),
	netip.MustParsePrefix("192.0.0.10/32"),
}

// Keep IPv6 translation prefixes outside 2000::/3 blocked: their embedded
// IPv4 destination can be private even when the prefix is globally reachable.
var httpPublicIPv6 = netip.MustParsePrefix("2000::/3")

var httpNonPublicIPv6 = []netip.Prefix{
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

var httpPublicSpecialIPv6 = []netip.Prefix{
	netip.MustParsePrefix("2001:1::1/128"),
	netip.MustParsePrefix("2001:1::2/128"),
	netip.MustParsePrefix("2001:1::3/128"),
	netip.MustParsePrefix("2001:3::/32"),
	netip.MustParsePrefix("2001:4:112::/48"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2001:30::/28"),
}

func (policy httpNetworkPolicy) allowedIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() {
		return false
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	if policy.allowPrivate {
		return true
	}
	address = address.Unmap()
	if address.Is6() && !httpPublicIPv6.Contains(address) {
		return false
	}
	blocked, exceptions := httpNonPublicIPv6, httpPublicSpecialIPv6
	if address.Is4() {
		blocked, exceptions = httpNonPublicIPv4, httpPublicSpecialIPv4
	}
	for _, prefix := range blocked {
		if prefix.Contains(address) {
			for _, exception := range exceptions {
				if exception.Contains(address) {
					return true
				}
			}
			return false
		}
	}
	return true
}
