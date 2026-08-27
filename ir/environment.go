// Package ir defines Effectus's callback-free, checked execution representation.
package ir

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// FormatVersion is the only artifact format accepted by this package.
const FormatVersion uint32 = 1

// Environment is immutable input to checking. Check copies it before use.
// It contains declarations only; checking never calls executors or user code.
type Environment struct {
	Facts     map[string]string           `json:"facts"`
	Verbs     map[string]VerbContract     `json:"verbs"`
	Functions map[string]FunctionContract `json:"functions"`
	Types     map[string]TypeDefinition   `json:"types"`
}

// VerbContract describes the serializable part of a verb contract.
type VerbContract struct {
	Arguments         map[string]string `json:"arguments"`
	RequiredArgs      []string          `json:"required_args"`
	ResultType        string            `json:"result_type"`
	InverseVerb       string            `json:"inverse_verb,omitempty"`
	RetryPolicy       RetryPolicy       `json:"retry_policy"`
	IdempotencyPolicy IdempotencyPolicy `json:"idempotency_policy"`
	FencingRequired   bool              `json:"fencing_required"`
}

// RetryPolicy is frozen into every checked step that invokes the verb.
type RetryPolicy struct {
	MaxAttempts          uint32 `json:"max_attempts"`
	InitialBackoffMillis uint64 `json:"initial_backoff_millis"`
	MaxBackoffMillis     uint64 `json:"max_backoff_millis"`
}

// IdempotencyPolicy states which retry guarantee an executor binding provides.
type IdempotencyPolicy string

const (
	IdempotencyNone           IdempotencyPolicy = "none"
	IdempotencyKeyRequired    IdempotencyPolicy = "key_required"
	IdempotencySinkGuaranteed IdempotencyPolicy = "sink_guaranteed"
)

// FunctionContract describes an expression function. Checked predicates may
// reference only functions explicitly declared pure and total.
type FunctionContract struct {
	ArgumentTypes []string `json:"argument_types"`
	ReturnType    string   `json:"return_type"`
	Pure          bool     `json:"pure"`
	Total         bool     `json:"total"`
}

// TypeKind identifies a named type definition.
type TypeKind string

const (
	TypeKindObject TypeKind = "object"
	TypeKindList   TypeKind = "list"
	TypeKindMap    TypeKind = "map"
)

// TypeDefinition declares a named structural type. Object fields not present
// in Fields are rejected. Maps always have string keys.
type TypeDefinition struct {
	Kind           TypeKind          `json:"kind"`
	ElementType    string            `json:"element_type,omitempty"`
	Fields         map[string]string `json:"fields,omitempty"`
	RequiredFields []string          `json:"required_fields,omitempty"`
}

// ContractHash returns the canonical SHA-256 digest of a verb contract.
func ContractHash(contract VerbContract) (string, error) {
	normalized, err := normalizeVerbContract(contract)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal verb contract: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// EnvironmentDigest returns the canonical SHA-256 digest used by artifacts.
func EnvironmentDigest(environment Environment) (string, error) {
	normalized, err := normalizeEnvironment(environment)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal environment: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func normalizeEnvironment(environment Environment) (Environment, error) {
	out := Environment{
		Facts:     make(map[string]string, len(environment.Facts)),
		Verbs:     make(map[string]VerbContract, len(environment.Verbs)),
		Functions: make(map[string]FunctionContract, len(environment.Functions)),
		Types:     make(map[string]TypeDefinition, len(environment.Types)),
	}
	for name, definition := range environment.Types {
		trimmedName := strings.TrimSpace(name)
		if trimmedName == "" {
			return Environment{}, fmt.Errorf("type name is empty")
		}
		if trimmedName != name {
			return Environment{}, fmt.Errorf("type name %q has leading or trailing whitespace", name)
		}
		if isReservedTypeName(name) {
			return Environment{}, fmt.Errorf("type %q shadows a built-in type", name)
		}
		definition.Kind = TypeKind(strings.ToLower(strings.TrimSpace(string(definition.Kind))))
		definition.ElementType = strings.TrimSpace(definition.ElementType)
		definition.Fields = cloneStringMap(definition.Fields)
		definition.RequiredFields = sortedStrings(definition.RequiredFields)
		out.Types[name] = definition
	}

	for path, typeName := range environment.Facts {
		trimmedPath := strings.TrimSpace(path)
		if trimmedPath == "" {
			return Environment{}, fmt.Errorf("fact path is empty")
		}
		if trimmedPath != path {
			return Environment{}, fmt.Errorf("fact path %q has leading or trailing whitespace", path)
		}
		out.Facts[path] = strings.TrimSpace(typeName)
	}
	for name, contract := range environment.Verbs {
		trimmedName := strings.TrimSpace(name)
		if trimmedName == "" {
			return Environment{}, fmt.Errorf("verb name is empty")
		}
		if trimmedName != name {
			return Environment{}, fmt.Errorf("verb name %q has leading or trailing whitespace", name)
		}
		normalized, err := normalizeVerbContract(contract)
		if err != nil {
			return Environment{}, fmt.Errorf("verb %q: %w", name, err)
		}
		out.Verbs[name] = normalized
	}
	for name, contract := range environment.Functions {
		trimmedName := strings.TrimSpace(name)
		if trimmedName == "" {
			return Environment{}, fmt.Errorf("function name is empty")
		}
		if trimmedName != name {
			return Environment{}, fmt.Errorf("function name %q has leading or trailing whitespace", name)
		}
		contract.ReturnType = strings.TrimSpace(contract.ReturnType)
		contract.ArgumentTypes = append(make([]string, 0, len(contract.ArgumentTypes)), contract.ArgumentTypes...)
		for i := range contract.ArgumentTypes {
			contract.ArgumentTypes[i] = strings.TrimSpace(contract.ArgumentTypes[i])
		}
		out.Functions[name] = contract
	}

	checker := typeChecker{environment: out}
	for name, definition := range out.Types {
		switch definition.Kind {
		case TypeKindObject:
			if definition.ElementType != "" {
				return Environment{}, fmt.Errorf("type %q: object cannot have an element type", name)
			}
			required := make(map[string]struct{}, len(definition.RequiredFields))
			for _, field := range definition.RequiredFields {
				if _, duplicate := required[field]; duplicate {
					return Environment{}, fmt.Errorf("type %q: duplicate required field %q", name, field)
				}
				required[field] = struct{}{}
				if _, ok := definition.Fields[field]; !ok {
					return Environment{}, fmt.Errorf("type %q: required field %q is not declared", name, field)
				}
			}
			for field, typeName := range definition.Fields {
				if strings.TrimSpace(field) == "" {
					return Environment{}, fmt.Errorf("type %q: field name is empty", name)
				}
				if strings.TrimSpace(field) != field {
					return Environment{}, fmt.Errorf("type %q: field name %q has leading or trailing whitespace", name, field)
				}
				if _, err := checker.parse(typeName, false); err != nil {
					return Environment{}, fmt.Errorf("type %q field %q: %w", name, field, err)
				}
			}
		case TypeKindList, TypeKindMap:
			if len(definition.Fields) != 0 || len(definition.RequiredFields) != 0 {
				return Environment{}, fmt.Errorf("type %q: %s cannot declare fields", name, definition.Kind)
			}
			if _, err := checker.parse(definition.ElementType, false); err != nil {
				return Environment{}, fmt.Errorf("type %q element: %w", name, err)
			}
		default:
			return Environment{}, fmt.Errorf("type %q has unsupported kind %q", name, definition.Kind)
		}
	}
	for path, typeName := range out.Facts {
		if _, err := checker.parse(typeName, false); err != nil {
			return Environment{}, fmt.Errorf("fact %q: %w", path, err)
		}
	}
	for name, contract := range out.Verbs {
		for argument, typeName := range contract.Arguments {
			if strings.TrimSpace(argument) == "" {
				return Environment{}, fmt.Errorf("verb %q has an empty argument name", name)
			}
			if _, err := checker.parse(typeName, false); err != nil {
				return Environment{}, fmt.Errorf("verb %q argument %q: %w", name, argument, err)
			}
		}
		if _, err := checker.parse(contract.ResultType, true); err != nil {
			return Environment{}, fmt.Errorf("verb %q result: %w", name, err)
		}
	}
	for name, contract := range out.Functions {
		for i, typeName := range contract.ArgumentTypes {
			if _, err := checker.parse(typeName, false); err != nil {
				return Environment{}, fmt.Errorf("function %q argument %d: %w", name, i, err)
			}
		}
		if _, err := checker.parse(contract.ReturnType, false); err != nil {
			return Environment{}, fmt.Errorf("function %q result: %w", name, err)
		}
	}
	return out, nil
}

func normalizeVerbContract(contract VerbContract) (VerbContract, error) {
	contract.Arguments = cloneStringMap(contract.Arguments)
	for name, typeName := range contract.Arguments {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || trimmed != name {
			return VerbContract{}, fmt.Errorf("invalid argument name %q", name)
		}
		contract.Arguments[name] = strings.TrimSpace(typeName)
	}
	if contract.RequiredArgs == nil {
		contract.RequiredArgs = make([]string, 0, len(contract.Arguments))
		for name := range contract.Arguments {
			contract.RequiredArgs = append(contract.RequiredArgs, name)
		}
	}
	contract.RequiredArgs = sortedStrings(contract.RequiredArgs)
	seen := make(map[string]struct{}, len(contract.RequiredArgs))
	for _, name := range contract.RequiredArgs {
		if _, duplicate := seen[name]; duplicate {
			return VerbContract{}, fmt.Errorf("duplicate required argument %q", name)
		}
		seen[name] = struct{}{}
		if _, ok := contract.Arguments[name]; !ok {
			return VerbContract{}, fmt.Errorf("required argument %q is not declared", name)
		}
	}
	contract.ResultType = strings.TrimSpace(contract.ResultType)
	contract.InverseVerb = strings.TrimSpace(contract.InverseVerb)
	if contract.RetryPolicy.MaxAttempts == 0 {
		contract.RetryPolicy.MaxAttempts = 1
	}
	if contract.RetryPolicy.MaxBackoffMillis != 0 && contract.RetryPolicy.InitialBackoffMillis > contract.RetryPolicy.MaxBackoffMillis {
		return VerbContract{}, fmt.Errorf("retry initial backoff exceeds maximum backoff")
	}
	if contract.IdempotencyPolicy == "" {
		contract.IdempotencyPolicy = IdempotencyNone
	}
	switch contract.IdempotencyPolicy {
	case IdempotencyNone, IdempotencyKeyRequired, IdempotencySinkGuaranteed:
	default:
		return VerbContract{}, fmt.Errorf("invalid idempotency policy %q", contract.IdempotencyPolicy)
	}
	return contract, nil
}

func cloneStringMap(input map[string]string) map[string]string {
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func sortedStrings(input []string) []string {
	out := append(make([]string, 0, len(input)), input...)
	sort.Strings(out)
	return out
}

func isReservedTypeName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bool", "boolean", "int", "integer", "float", "double", "number", "string", "bytes", "null", "void", "any", "unknown", "object", "list", "map":
		return true
	default:
		return false
	}
}
