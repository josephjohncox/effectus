package loader

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type IPResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type OutboundNetworkPolicy struct {
	AllowPrivate bool
	Resolver     IPResolver
	MaxRedirects int
	DialTimeout  time.Duration
}

func (policy OutboundNetworkPolicy) ValidateURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse outbound URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("outbound URL scheme must be http or https")
	}
	if parsed.Hostname() == "" || parsed.User != nil {
		return nil, fmt.Errorf("outbound URL must have a host and no user information")
	}
	if strings.EqualFold(parsed.Hostname(), "localhost") && !policy.AllowPrivate {
		return nil, fmt.Errorf("outbound URL host is not allowed")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && !policy.allowedIP(ip) {
		return nil, fmt.Errorf("outbound URL IP is not allowed")
	}
	return parsed, nil
}

func (policy OutboundNetworkPolicy) HTTPClient(timeout time.Duration, sensitiveHeaders map[string]string) *http.Client {
	if policy.Resolver == nil {
		policy.Resolver = net.DefaultResolver
	}
	if policy.MaxRedirects <= 0 {
		policy.MaxRedirects = 3
	}
	if policy.DialTimeout <= 0 {
		policy.DialTimeout = 5 * time.Second
	}
	dialer := &net.Dialer{Timeout: policy.DialTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{ForceAttemptHTTP2: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := policy.Resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve outbound host: %w", err)
		}
		if len(addresses) == 0 {
			return nil, fmt.Errorf("outbound host has no addresses")
		}
		for _, candidate := range addresses {
			if !policy.allowedIP(candidate.IP) {
				return nil, fmt.Errorf("outbound host resolved to a disallowed address")
			}
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
		if len(via) >= policy.MaxRedirects {
			return fmt.Errorf("outbound redirect limit exceeded")
		}
		if _, err := policy.ValidateURL(request.URL.String()); err != nil {
			return err
		}
		if len(via) > 0 && strings.EqualFold(via[len(via)-1].URL.Scheme, "https") && strings.EqualFold(request.URL.Scheme, "http") {
			return fmt.Errorf("outbound HTTPS redirect cannot downgrade to HTTP")
		}
		if len(via) > 0 && !sameURLOrigin(via[0].URL, request.URL) {
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
	}}
}

func sameURLOrigin(left, right *url.URL) bool {
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

func (policy OutboundNetworkPolicy) allowedIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() {
		return false
	}
	if !policy.AllowPrivate && (ip.IsPrivate() || ip.IsLoopback()) {
		return false
	}
	return true
}
