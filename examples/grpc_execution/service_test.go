package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/embedded"
	"github.com/josephjohncox/effectus/internal/demo/orderreview"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/runtime"
	"github.com/stretchr/testify/require"
)

const exampleTestToken = "example-test-token-not-a-deployed-credential"

type exampleExecutor struct{ calls atomic.Int64 }

func (executor *exampleExecutor) Invoke(ctx context.Context, request invocation.Request) invocation.Outcome {
	if err := ctx.Err(); err != nil {
		return invocation.Outcome{Class: invocation.OutcomeRetryableKnownNotCommitted, Err: err}
	}
	if request.Arguments["orderId"] != "order-200" || request.Arguments["reason"] != orderreview.Reason {
		return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: fmt.Errorf("unexpected example arguments")}
	}
	executor.calls.Add(1)
	return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
}

// This is an in-memory fixture, not a durable destination or a production daemon.
// Counting calls verifies execution replay; it does not prove crash-safe effects.
func exampleService(t *testing.T, transport *tls.Config) (string, *exampleExecutor) {
	t.Helper()
	rule, err := orderreview.RuleSource()
	require.NoError(t, err)
	const resolverID = "example/grpc-execution/v1"
	descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorEmbedded, ResolverID: resolverID, Reference: "order-review"})
	require.NoError(t, err)
	source, err := bundle.New(bundle.Spec{
		Name: "order-review", Version: "1.0.0",
		Sources: []bundle.Source{{Path: "rules/order_review.eff", Content: string(rule)}},
		Environment: ir.Environment{
			Facts: map[string]string{"order.id": "string", "order.total": "float", "order.risk_score": "int"},
			Verbs: map[string]ir.VerbContract{orderreview.VerbName: {Arguments: map[string]string{"orderId": "string", "reason": "string"}, RequiredArgs: []string{"orderId", "reason"}, ResultType: "bool"}},
		},
		Executors: map[string]invocation.Descriptor{orderreview.VerbName: descriptor},
	})
	require.NoError(t, err)
	executor := &exampleExecutor{}
	registry, err := invocation.NewRegistry([]invocation.ResolverRegistration{{ID: resolverID, Resolver: invocation.ResolverFunc(func(context.Context, invocation.Descriptor) (invocation.Executor, io.Closer, error) {
		return executor, nil, nil
	})}})
	require.NoError(t, err)
	runner, err := embedded.Open(t.Context(), source, registry)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runner.Close()) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	auth, err := runtime.NewBearerTokenAuthenticator(exampleTestToken)
	require.NoError(t, err)
	server, err := runtime.NewRulesetExecutionServerOnListener(runner.Engine(), listener, runtime.RulesetExecutionServerOptions{
		RulesetName: "order-review", Version: "1.0.0", Authenticator: auth,
		TLSConfig: transport, AllowInsecureTransport: transport == nil,
	})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- server.Start() }()
	t.Cleanup(func() {
		server.Stop()
		require.NoError(t, <-done)
	})
	return listener.Addr().String(), executor
}

func exampleCertificate(t *testing.T, matchHost bool) (*tls.Config, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "effectus example test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:        true, BasicConstraintsValid: true, DNSNames: []string{"wrong-host.invalid"},
	}
	if matchHost {
		certificate.DNSNames = []string{"localhost"}
		certificate.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	require.NoError(t, err)
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}, caPath
}
