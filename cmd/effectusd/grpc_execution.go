package main

import (
	"crypto/tls"
	"fmt"
	"strings"

	effectusruntime "github.com/effectus/effectus-go/runtime"
	"github.com/effectus/effectus-go/unified"
)

func configureDaemonGRPCServer(execution *effectusruntime.ExecutionRuntime, bundle *unified.Bundle) (*effectusruntime.RulesetExecutionServer, error) {
	if execution == nil || bundle == nil {
		return nil, fmt.Errorf("gRPC checked runtime and bundle are required")
	}
	options := effectusruntime.RulesetExecutionServerOptions{
		RulesetName: bundle.Name, Version: bundle.Version,
		AllowInsecureTransport: *grpcAllowInsecure,
		MaxReceiveBytes:        *grpcMaxReceive, MaxSendBytes: *grpcMaxSend,
		MaxExecutionDuration: *grpcMaxDuration, MaxConcurrentRPCs: *grpcMaxConcurrent,
	}
	if strings.EqualFold(strings.TrimSpace(*apiAuthMode), "disabled") {
		options.AllowUnauthenticated = true
	} else {
		authenticator, err := effectusruntime.NewBearerTokenAuthenticatorSet(splitCommaList(*apiToken))
		if err != nil {
			return nil, fmt.Errorf("configure gRPC authentication: %w", err)
		}
		options.Authenticator = authenticator
	}
	certPath, keyPath := strings.TrimSpace(*grpcTLSCert), strings.TrimSpace(*grpcTLSKey)
	if certPath != "" || keyPath != "" {
		if certPath == "" || keyPath == "" {
			return nil, fmt.Errorf("both gRPC TLS certificate and key are required")
		}
		certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("load gRPC TLS identity: %w", err)
		}
		options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	}
	return effectusruntime.NewRulesetExecutionServerWithOptions(execution, *grpcAddr, options)
}
