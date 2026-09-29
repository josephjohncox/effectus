package invocation

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHTTPResolverClassifiesLiteralAndResolvedAddresses(t *testing.T) {
	tests := []struct {
		address string
		allowed bool
	}{
		{"1.1.1.1", true},
		{"100.128.0.1", true},
		{"192.0.0.9", true},
		{"192.0.0.10", true},
		{"192.31.196.1", true},
		{"192.52.193.1", true},
		{"192.175.48.1", true},
		{"::ffff:192.0.0.9", true},
		{"198.20.0.1", true},
		{"2001:1::1", true},
		{"2001:1::2", true},
		{"2001:1::3", true},
		{"2001:3::1", true},
		{"2001:4:112::1", true},
		{"2001:20::1", true},
		{"2001:30::1", true},
		{"2620:4f:8000::1", true},
		{"2606:4700:4700::1111", true},
		{"10.1.2.3", false},
		{"100.64.0.1", false},
		{"100.127.255.254", false},
		{"127.0.0.1", false},
		{"192.0.0.8", false},
		{"192.0.0.11", false},
		{"192.0.2.1", false},
		{"192.88.99.1", false},
		{"198.18.0.1", false},
		{"198.19.255.254", false},
		{"198.51.100.1", false},
		{"203.0.113.1", false},
		{"240.0.0.1", false},
		{"fd00::1", false},
		{"64:ff9b::c0a8:101", false},
		{"64:ff9b:1::1", false},
		{"100::1", false},
		{"2001:1::4", false},
		{"2001:2::1", false},
		{"2001:db8::1", false},
		{"2002:c0a8:101::1", false},
		{"3fff::1", false},
		{"5f00::1", false},
		{"::ffff:100.64.0.1", false},
	}
	policy := httpNetworkPolicy{}
	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			host := test.address
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
			_, literalErr := policy.validateURL("http://" + host + "/invoke")
			resolvedErr := policy.validateResolvedIPs([]net.IPAddr{{IP: net.ParseIP(test.address)}})
			if test.allowed {
				require.NoError(t, literalErr)
				require.NoError(t, resolvedErr)
			} else {
				require.ErrorContains(t, literalErr, "IP is not allowed")
				require.ErrorContains(t, resolvedErr, "disallowed address")
			}
		})
	}
	require.ErrorContains(t, policy.validateResolvedIPs([]net.IPAddr{{IP: net.ParseIP("1.1.1.1")}, {IP: net.ParseIP("100.64.0.1")}}), "disallowed address")
	require.ErrorContains(t, policy.validateResolvedIPs(nil), "no addresses")
}

func TestHTTPResolverPrivateNetworkOptInAndAlwaysDeniedAddresses(t *testing.T) {
	policy := httpNetworkPolicy{allowPrivate: true}
	for _, address := range []string{"127.0.0.1", "10.1.2.3", "100.64.0.1", "198.18.0.1", "fd00::1"} {
		require.NoError(t, policy.validateResolvedIPs([]net.IPAddr{{IP: net.ParseIP(address)}}), address)
	}
	for _, address := range []string{"0.0.0.0", "169.254.1.1", "224.0.0.1", "::", "fe80::1", "ff02::1"} {
		require.ErrorContains(t, policy.validateResolvedIPs([]net.IPAddr{{IP: net.ParseIP(address)}}), "disallowed address", address)
	}
	require.ErrorContains(t, policy.validateResolvedIPs([]net.IPAddr{{}}), "disallowed address")

	resolver := HTTPResolver{}
	for _, allowPrivate := range []bool{false, true} {
		settings := map[string]string{}
		if allowPrivate {
			settings["allow_private_network"] = "true"
		}
		descriptor, err := NewDescriptor(DescriptorSpec{
			Type: DescriptorHTTP, ResolverID: HTTPResolverID, Reference: "http://100.64.0.1/invoke", Settings: settings,
		})
		require.NoError(t, err)
		_, closer, err := resolver.Resolve(context.Background(), descriptor)
		if allowPrivate {
			require.NoError(t, err)
			require.NotNil(t, closer)
			require.NoError(t, closer.Close())
		} else {
			require.ErrorContains(t, err, "IP is not allowed")
			require.Nil(t, closer)
		}
	}
}

func TestHTTPResolverClosesIdleConnections(t *testing.T) {
	idle := make(chan struct{}, 1)
	closed := make(chan struct{}, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		switch state {
		case http.StateIdle:
			select {
			case idle <- struct{}{}:
			default:
			}
		case http.StateClosed:
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	descriptor, err := NewDescriptor(DescriptorSpec{
		Type: DescriptorHTTP, ResolverID: HTTPResolverID, Reference: server.URL,
		Settings: map[string]string{"allow_private_network": "true"},
	})
	require.NoError(t, err)
	executor, closer, err := (HTTPResolver{}).Resolve(context.Background(), descriptor)
	require.NoError(t, err)
	require.NotNil(t, closer)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	resolved := executor.(*describedHTTPExecutor)
	transport := resolved.Client.Transport.(*http.Transport)
	require.Greater(t, transport.IdleConnTimeout, time.Duration(0))
	require.Equal(t, OutcomeSuccess, executor.Invoke(context.Background(), Request{}).Class)
	select {
	case <-idle:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP connection did not become idle")
	}
	require.NoError(t, closer.Close())
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("resolver closer did not close its idle connection")
	}
}
