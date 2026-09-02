package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/schema/verb"
)

type sourceVerbManifest struct {
	Name        string                  `json:"name"`
	Version     string                  `json:"version"`
	Description string                  `json:"description"`
	Verbs       []sourceVerbDeclaration `json:"verbs"`
}

type sourceVerbDeclaration struct {
	Name         string               `json:"name"`
	Description  string               `json:"description"`
	Capabilities []string             `json:"capabilities"`
	ArgTypes     map[string]string    `json:"argTypes"`
	RequiredArgs []string             `json:"requiredArgs"`
	ReturnType   string               `json:"returnType"`
	InverseVerb  string               `json:"inverseVerb"`
	Resources    []sourceVerbResource `json:"resources"`
	Target       *sourceVerbTarget    `json:"target"`
}

type sourceVerbResource struct {
	Resource     string   `json:"resource"`
	Capabilities []string `json:"capabilities"`
}

type sourceVerbTarget struct {
	Type   string          `json:"type"`
	Ref    string          `json:"ref"`
	Config json.RawMessage `json:"config"`
}

type sourceHTTPConfig struct {
	URL                 string            `json:"url"`
	Method              string            `json:"method"`
	Timeout             string            `json:"timeout"`
	AllowPrivateNetwork bool              `json:"allowPrivateNetwork"`
	Headers             map[string]string `json:"headers"`
}

type sourceGRPCConfig struct {
	Address          string            `json:"address"`
	Method           string            `json:"method"`
	Timeout          string            `json:"timeout"`
	Metadata         map[string]string `json:"metadata"`
	TLS              bool              `json:"tls"`
	Insecure         bool              `json:"insecure"`
	ServerName       string            `json:"serverName"`
	DescriptorDigest string            `json:"descriptorDigest"`
}

type sourceOCIConfig struct {
	Ref               string `json:"ref"`
	Verb              string `json:"verb"`
	SignatureVerifier string `json:"signatureVerifier"`
}

func loadSourceManifestVerbs(inputs []string, registry *verb.Registry) error {
	if registry == nil {
		return fmt.Errorf("verb registry is nil")
	}
	for _, filename := range expandSchemaPaths(inputs) {
		data, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("read verb manifest %s: %w", filename, err)
		}
		var probe struct {
			Verbs json.RawMessage `json:"verbs"`
		}
		if err := json.Unmarshal(data, &probe); err != nil || len(probe.Verbs) == 0 {
			continue
		}
		var manifest sourceVerbManifest
		if err := strictTransportConfig(data, &manifest); err != nil {
			return fmt.Errorf("decode verb manifest %s: %w", filename, err)
		}
		for _, declaration := range manifest.Verbs {
			capability, err := sourceVerbCapability(declaration.Capabilities)
			if err != nil {
				return fmt.Errorf("verb %q in %s: %w", declaration.Name, filename, err)
			}
			resources := make(verb.ResourceSet, 0, len(declaration.Resources))
			for _, resource := range declaration.Resources {
				resourceCapability, resourceErr := sourceVerbCapability(resource.Capabilities)
				if resourceErr != nil {
					return fmt.Errorf("verb %q resource %q in %s: %w", declaration.Name, resource.Resource, filename, resourceErr)
				}
				resources = append(resources, verb.ResourceCapability{Resource: resource.Resource, Cap: resourceCapability})
			}
			spec := verb.NewSpec(declaration.Name, capability, declaration.ArgTypes, declaration.ReturnType).
				WithDescription(declaration.Description).
				WithRequiredArgs(declaration.RequiredArgs).
				WithInverse(declaration.InverseVerb).
				WithResources(resources)
			if err := registry.RegisterVerb(spec); err != nil {
				return fmt.Errorf("register verb %q from %s: %w", declaration.Name, filename, err)
			}
		}
	}
	return nil
}

func sourceVerbCapability(values []string) (verb.Capability, error) {
	var capability verb.Capability
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "read":
			capability |= verb.CapRead
		case "write":
			capability |= verb.CapWrite
		case "create":
			capability |= verb.CapCreate
		case "delete":
			capability |= verb.CapDelete
		case "idempotent":
			capability |= verb.CapIdempotent
		case "exclusive":
			capability |= verb.CapExclusive
		case "commutative":
			capability |= verb.CapCommutative
		case "", "default":
			capability |= verb.CapDefault
		default:
			return 0, fmt.Errorf("unknown capability %q", value)
		}
	}
	return capability, nil
}

func loadBundleDescriptors(inputs []string) (map[string]invocation.Descriptor, error) {
	result := make(map[string]invocation.Descriptor)
	for _, filename := range expandSchemaPaths(inputs) {
		data, err := os.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("read verb descriptor manifest %s: %w", filename, err)
		}
		var probe struct {
			Verbs json.RawMessage `json:"verbs"`
		}
		if err := json.Unmarshal(data, &probe); err != nil || len(probe.Verbs) == 0 {
			continue // A declaration-only verb schema has no invocation targets.
		}
		var manifest sourceVerbManifest
		if err := strictTransportConfig(data, &manifest); err != nil {
			return nil, fmt.Errorf("decode verb descriptor manifest %s: %w", filename, err)
		}
		for _, declaration := range manifest.Verbs {
			if declaration.Target == nil {
				continue
			}
			if _, duplicate := result[declaration.Name]; duplicate {
				return nil, fmt.Errorf("verb invocation descriptor %q is declared more than once", declaration.Name)
			}
			descriptor, err := sourceInvocationDescriptor(declaration)
			if err != nil {
				return nil, fmt.Errorf("verb %q in %s: %w", declaration.Name, filename, err)
			}
			result[declaration.Name] = descriptor
		}
	}
	return result, nil
}

func sourceInvocationDescriptor(declaration sourceVerbDeclaration) (invocation.Descriptor, error) {
	targetType := strings.ToLower(strings.TrimSpace(declaration.Target.Type))
	switch targetType {
	case "http":
		var config sourceHTTPConfig
		if err := strictTransportConfig(declaration.Target.Config, &config); err != nil {
			return invocation.Descriptor{}, err
		}
		return invocation.NewDescriptor(invocation.DescriptorSpec{
			Type: invocation.DescriptorHTTP, ResolverID: invocation.HTTPResolverID, Reference: config.URL,
			Headers: config.Headers,
			Settings: map[string]string{
				"method": config.Method, "timeout": config.Timeout,
				"allow_private_network": strconv.FormatBool(config.AllowPrivateNetwork),
			},
		})
	case "grpc", "stream", "message", "oci":
		return invocation.Descriptor{}, fmt.Errorf("source bundles support only the canonical HTTP invocation target; %q is compatibility-only", targetType)
	default:
		return invocation.Descriptor{}, fmt.Errorf("unsupported production executor target %q", declaration.Target.Type)
	}
}

func strictTransportConfig(data json.RawMessage, target any) error {
	if len(data) == 0 {
		data = []byte(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode executor target config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode executor target config: trailing JSON value")
		}
		return fmt.Errorf("decode executor target config: %w", err)
	}
	return nil
}
