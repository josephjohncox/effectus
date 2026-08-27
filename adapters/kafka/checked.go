package kafka

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	effectusruntime "github.com/effectus/effectus-go/runtime"
	"github.com/effectus/effectus-go/schema"
)

// ExecuteEngine is the shared runtime.Engine surface used by Kafka.
type ExecuteEngine interface {
	Execute(context.Context, effectusruntime.ExecuteRequest) (effectusruntime.ExecuteResult, error)
}

// EngineHandlerConfig maps Kafka delivery semantics to Engine.Execute.
type EngineHandlerConfig struct {
	Ruleset         string
	Version         string
	DefaultTenant   string
	MaxMessageBytes int
	WaitMode        effectusruntime.WaitMode
}

type engineHandler struct {
	config EngineHandlerConfig
	engine ExecuteEngine
}

// NewEngineHandler creates a Kafka Handler that admits every record through
// runtime.Engine.Execute. No artifact or transport-specific checked request is
// accepted at this boundary.
func NewEngineHandler(config EngineHandlerConfig, engine ExecuteEngine) (Handler, error) {
	if engine == nil {
		return nil, fmt.Errorf("runtime engine is required")
	}
	if strings.TrimSpace(config.Ruleset) == "" || strings.TrimSpace(config.Version) == "" {
		return nil, fmt.Errorf("ruleset and version are required")
	}
	if config.DefaultTenant == "" {
		config.DefaultTenant = "default"
	}
	if config.MaxMessageBytes <= 0 {
		config.MaxMessageBytes = 1 << 20
	}
	if config.WaitMode == "" {
		config.WaitMode = effectusruntime.WaitAccepted
	}
	if config.WaitMode != effectusruntime.WaitAccepted && config.WaitMode != effectusruntime.WaitTerminal {
		return nil, fmt.Errorf("unsupported Kafka wait mode %q", config.WaitMode)
	}
	return &engineHandler{config: config, engine: engine}, nil
}

func (handler *engineHandler) Handle(ctx context.Context, delivery Delivery) (HandleResult, error) {
	if len(delivery.Message.Value) > handler.config.MaxMessageBytes {
		return HandleResult{}, fmt.Errorf("Kafka fact message exceeds %d bytes", handler.config.MaxMessageBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(delivery.Message.Value))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var envelope struct {
		TenantNamespace string          `json:"namespace"`
		Universe        string          `json:"universe,omitempty"`
		ReceivedAt      json.RawMessage `json:"received_at,omitempty"`
		Facts           map[string]any  `json:"facts"`
	}
	if err := decoder.Decode(&envelope); err != nil {
		return HandleResult{}, fmt.Errorf("decode Kafka facts: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return HandleResult{}, fmt.Errorf("decode Kafka facts: multiple JSON values")
		}
		return HandleResult{}, fmt.Errorf("decode Kafka facts: %w", err)
	}
	if len(envelope.Facts) == 0 {
		return HandleResult{}, fmt.Errorf("Kafka facts are required")
	}
	if envelope.TenantNamespace == "" {
		envelope.TenantNamespace = handler.config.DefaultTenant
	}
	executionID := schema.StableExecutionID(
		envelope.TenantNamespace, delivery.ID, handler.config.Ruleset, handler.config.Version,
	)
	result, err := handler.engine.Execute(ctx, effectusruntime.ExecuteRequest{
		Admission: &effectusruntime.Admission{
			ExecutionID: executionID, AdmissionID: delivery.ID, TenantNamespace: envelope.TenantNamespace,
			Ruleset: handler.config.Ruleset, Version: handler.config.Version, Facts: envelope.Facts,
		},
		WaitMode: handler.config.WaitMode,
	})
	if err != nil {
		return HandleResult{}, err
	}
	return HandleResult{DurablyAccepted: result.DurablyAccepted, Completed: result.Completed}, nil
}

// CheckedHandlerConfig and NewCheckedHandler are compatibility facades. They
// no longer accept an artifact or a Kafka-specific execution request.
type CheckedHandlerConfig = EngineHandlerConfig

func NewCheckedHandler(config CheckedHandlerConfig, engine ExecuteEngine) (Handler, error) {
	return NewEngineHandler(config, engine)
}
