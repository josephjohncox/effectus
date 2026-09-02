package main

import (
	"crypto/tls"
	"fmt"
	"strings"

	effectusruntime "github.com/josephjohncox/effectus/runtime"
)

func configureDaemonGRPCServer(execution *effectusruntime.ExecutionRuntime) (*effectusruntime.RulesetExecutionServer, error) {
	if execution == nil || execution.Engine().GenerationView() == nil {
		return nil, fmt.Errorf("active checked generation is required for gRPC")
	}
	generation := execution.Engine().GenerationView()
	options := effectusruntime.RulesetExecutionServerOptions{
		RulesetName: generation.Ruleset, Version: generation.Version,
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
