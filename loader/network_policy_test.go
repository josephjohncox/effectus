package loader

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type staticIPResolver []net.IPAddr

func (resolver staticIPResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return append([]net.IPAddr(nil), resolver...), nil
}

func TestOutboundNetworkPolicyRejectsSSRFAddresses(t *testing.T) {
	policy := OutboundNetworkPolicy{}
	for _, target := range []string{"http://127.0.0.1/x", "http://10.0.0.1/x", "http://169.254.169.254/latest", "file:///etc/passwd", "http://user:pass@example.com"} {
		_, err := policy.ValidateURL(target)
		require.Error(t, err, target)
	}
	_, err := policy.ValidateURL("https://example.com/path")
	require.NoError(t, err)
}

func TestOutboundNetworkPolicyRejectsMixedDNSAnswers(t *testing.T) {
	policy := OutboundNetworkPolicy{Resolver: staticIPResolver{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("127.0.0.1")}}, DialTimeout: 10 * time.Millisecond}
	client := policy.HTTPClient(100*time.Millisecond, nil)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://public.example/", nil)
	require.NoError(t, err)
	_, err = client.Do(request)
	require.ErrorContains(t, err, "disallowed address")
}

func TestOutboundRedirectRevalidatesDestination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "http://169.254.169.254/latest", http.StatusFound)
	}))
	defer server.Close()
	policy := OutboundNetworkPolicy{AllowPrivate: true}
	client := policy.HTTPClient(time.Second, nil)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	_, err = client.Do(request)
	require.ErrorContains(t, err, "not allowed")
}
