package runtime

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var (
	ErrGRPCInvalidInput      = errors.New("invalid gRPC execution input")
	ErrGRPCUnauthorized      = errors.New("gRPC authentication failed")
	ErrGRPCUnavailable       = errors.New("gRPC execution unavailable")
	ErrGRPCResourceExhausted = errors.New("gRPC execution resource exhausted")
)

const (
	defaultGRPCMessageBytes  = 4 << 20
	defaultGRPCTimeout       = 30 * time.Second
	defaultGRPCConcurrentRPC = 128
)

type GRPCAuthenticator interface {
	Authenticate(context.Context, string) (context.Context, error)
}

type GRPCAuthenticatorFunc func(context.Context, string) (context.Context, error)

func (function GRPCAuthenticatorFunc) Authenticate(ctx context.Context, method string) (context.Context, error) {
	return function(ctx, method)
}

type BearerTokenAuthenticator struct{ tokens [][]byte }

func NewBearerTokenAuthenticator(token string) (*BearerTokenAuthenticator, error) {
	return NewBearerTokenAuthenticatorSet([]string{token})
}

func NewBearerTokenAuthenticatorSet(tokens []string) (*BearerTokenAuthenticator, error) {
	authenticator := &BearerTokenAuthenticator{}
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		authenticator.tokens = append(authenticator.tokens, []byte(token))
	}
	if len(authenticator.tokens) == 0 {
		return nil, fmt.Errorf("at least one bearer token is required")
	}
	return authenticator, nil
}

func (authenticator *BearerTokenAuthenticator) Authenticate(ctx context.Context, _ string) (context.Context, error) {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return nil, ErrGRPCUnauthorized
	}
	candidate := []byte(strings.TrimPrefix(values[0], "Bearer "))
	matched := 0
	for _, token := range authenticator.tokens {
		equalLength := subtle.ConstantTimeEq(int32(len(candidate)), int32(len(token)))
		comparison := 0
		if len(candidate) == len(token) {
			comparison = subtle.ConstantTimeCompare(candidate, token)
		}
		matched |= equalLength & comparison
	}
	if matched != 1 {
		return nil, ErrGRPCUnauthorized
	}
	return ctx, nil
}

// RulesetExecutionServerOptions defines the immutable service registration and
// transport policy. The generated service is registered in the constructor,
// before Serve can be called.
type RulesetExecutionServerOptions struct {
	MaxReceiveBytes        int
	MaxSendBytes           int
	MaxExecutionDuration   time.Duration
	MaxConcurrentRPCs      int
	Authenticator          GRPCAuthenticator
	AllowUnauthenticated   bool
	TLSConfig              *tls.Config
	AllowInsecureTransport bool
	RulesetName            string
	Version                string
}

func normalizeGRPCOptions(options RulesetExecutionServerOptions) (RulesetExecutionServerOptions, error) {
	if options.MaxReceiveBytes < 0 || options.MaxSendBytes < 0 || options.MaxExecutionDuration < 0 || options.MaxConcurrentRPCs < 0 {
		return RulesetExecutionServerOptions{}, fmt.Errorf("gRPC execution limits must not be negative")
	}
	if options.Authenticator == nil && !options.AllowUnauthenticated {
		return RulesetExecutionServerOptions{}, fmt.Errorf("gRPC authenticator is required unless unauthenticated access is explicitly allowed")
	}
	if options.TLSConfig == nil && !options.AllowInsecureTransport {
		return RulesetExecutionServerOptions{}, fmt.Errorf("gRPC TLS configuration is required unless insecure transport is explicitly allowed")
	}
	if options.MaxReceiveBytes == 0 {
		options.MaxReceiveBytes = defaultGRPCMessageBytes
	}
	if options.MaxSendBytes == 0 {
		options.MaxSendBytes = defaultGRPCMessageBytes
	}
	if options.MaxExecutionDuration == 0 {
		options.MaxExecutionDuration = defaultGRPCTimeout
	}
	if options.MaxConcurrentRPCs == 0 {
		options.MaxConcurrentRPCs = defaultGRPCConcurrentRPC
	}
	if strings.TrimSpace(options.RulesetName) == "" || strings.TrimSpace(options.Version) == "" {
		return RulesetExecutionServerOptions{}, fmt.Errorf("gRPC ruleset name and version are required")
	}
	if options.TLSConfig != nil {
		options.TLSConfig = options.TLSConfig.Clone()
		if options.TLSConfig.MinVersion == 0 || options.TLSConfig.MinVersion < tls.VersionTLS12 {
			options.TLSConfig.MinVersion = tls.VersionTLS12
		}
		if len(options.TLSConfig.Certificates) == 0 && options.TLSConfig.GetCertificate == nil {
			return RulesetExecutionServerOptions{}, fmt.Errorf("gRPC TLS server certificate is required")
		}
	}
	return options, nil
}

type RulesetExecutionServer struct {
	server   *grpc.Server
	listener net.Listener
	options  RulesetExecutionServerOptions
	mu       sync.Mutex
	started  bool
	stopped  bool
	serveErr error
}

func NewRulesetExecutionServer(engine *Engine, addr string) (*RulesetExecutionServer, error) {
	return NewRulesetExecutionServerWithOptions(engine, addr, RulesetExecutionServerOptions{})
}

func NewRulesetExecutionServerWithOptions(engine *Engine, addr string, options RulesetExecutionServerOptions) (*RulesetExecutionServer, error) {
	if strings.TrimSpace(addr) == "" {
		return nil, fmt.Errorf("gRPC listen address is required")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen for gRPC execution: %w", err)
	}
	server, err := NewRulesetExecutionServerOnListener(engine, listener, options)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	return server, nil
}

func NewRulesetExecutionServerOnListener(engine *Engine, listener net.Listener, options RulesetExecutionServerOptions) (*RulesetExecutionServer, error) {
	if engine == nil || engine.Generation() == nil {
		return nil, fmt.Errorf("checked execution runtime is required")
	}
	if listener == nil {
		return nil, fmt.Errorf("gRPC listener is required")
	}
	resolved, err := normalizeGRPCOptions(options)
	if err != nil {
		return nil, err
	}
	admission := make(chan struct{}, resolved.MaxConcurrentRPCs)
	grpcOptions := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(resolved.MaxReceiveBytes), grpc.MaxSendMsgSize(resolved.MaxSendBytes),
		grpc.UnaryInterceptor(stableExecutionUnaryInterceptor(resolved, admission)),
	}
	if resolved.TLSConfig != nil {
		grpcOptions = append(grpcOptions, grpc.Creds(credentials.NewTLS(resolved.TLSConfig)))
	}
	grpcServer := grpc.NewServer(grpcOptions...)
	if err := RegisterEngineExecutionServiceWithOptions(grpcServer, engine, EngineExecutionServiceOptions{RulesetName: resolved.RulesetName, Version: resolved.Version}); err != nil {
		return nil, err
	}
	return &RulesetExecutionServer{server: grpcServer, listener: listener, options: resolved}, nil
}

func stableExecutionUnaryInterceptor(options RulesetExecutionServerOptions, admission chan struct{}) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		select {
		case admission <- struct{}{}:
			defer func() { <-admission }()
		default:
			return nil, status.Error(codes.ResourceExhausted, "execution capacity is exhausted")
		}
		if options.Authenticator != nil {
			authenticated, err := options.Authenticator.Authenticate(ctx, info.FullMethod)
			if err != nil {
				return nil, status.Error(codes.Unauthenticated, "authentication failed")
			}
			ctx = authenticated
		}
		deadline := time.Now().Add(options.MaxExecutionDuration)
		if existing, ok := ctx.Deadline(); !ok || existing.After(deadline) {
			var cancel context.CancelFunc
			ctx, cancel = context.WithDeadline(ctx, deadline)
			defer cancel()
		}
		response, err := handler(ctx, request)
		return response, sanitizeGRPCStatus(err)
	}
}

func sanitizeGRPCStatus(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, "request canceled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "execution deadline exceeded")
	}
	if value, ok := status.FromError(err); ok {
		switch value.Code() {
		case codes.InvalidArgument, codes.Unauthenticated, codes.PermissionDenied, codes.NotFound, codes.AlreadyExists,
			codes.FailedPrecondition, codes.ResourceExhausted, codes.Unimplemented, codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
			return status.Error(value.Code(), value.Message())
		}
	}
	return status.Error(codes.Internal, "execution failed")
}

func (server *RulesetExecutionServer) Ready() error {
	if server == nil {
		return fmt.Errorf("gRPC execution service is unavailable")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.serveErr != nil {
		return server.serveErr
	}
	if !server.started || server.stopped {
		return fmt.Errorf("gRPC execution service is not serving")
	}
	return nil
}

func (server *RulesetExecutionServer) Address() net.Addr {
	if server == nil || server.listener == nil {
		return nil
	}
	return server.listener.Addr()
}
func (server *RulesetExecutionServer) Start() error {
	server.mu.Lock()
	if server.started || server.stopped {
		server.mu.Unlock()
		return fmt.Errorf("gRPC execution server cannot be started in its current state")
	}
	server.started = true
	server.mu.Unlock()
	log.Printf("Effectus generated gRPC execution service listening on %s", server.listener.Addr())
	err := server.server.Serve(server.listener)
	if errors.Is(err, grpc.ErrServerStopped) {
		return nil
	}
	if err != nil {
		server.mu.Lock()
		server.serveErr = err
		server.mu.Unlock()
	}
	return err
}
func (server *RulesetExecutionServer) Stop() {
	if server == nil || server.server == nil {
		return
	}
	server.mu.Lock()
	if server.stopped {
		server.mu.Unlock()
		return
	}
	server.stopped = true
	started := server.started
	server.mu.Unlock()
	if !started {
		_ = server.listener.Close()
		return
	}
	done := make(chan struct{})
	go func() { server.server.GracefulStop(); close(done) }()
	timer := time.NewTimer(server.options.MaxExecutionDuration)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		server.server.Stop()
	}
}
