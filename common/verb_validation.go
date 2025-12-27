package common

import (
	"fmt"
	"os"
	"strings"

	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
)

const (
	envStrictArgs   = "EFFECTUS_STRICT_VERB_ARGS"
	envStrictReturn = "EFFECTUS_STRICT_VERB_RETURN"
)

type verbRegistryStrictness interface {
	StrictArgs() *bool
	StrictReturn() *bool
}

func resolveStrictArgs(spec *verb.Spec, registry VerbRegistry) bool {
	if value, ok := envBool(envStrictArgs); ok {
		return value
	}
	if spec != nil && spec.StrictArgs != nil {
		return *spec.StrictArgs
	}
	if reg, ok := registry.(verbRegistryStrictness); ok {
		if reg.StrictArgs() != nil {
			return *reg.StrictArgs()
		}
	}
	return false
}

func resolveStrictReturn(spec *verb.Spec, registry VerbRegistry) bool {
	if value, ok := envBool(envStrictReturn); ok {
		return value
	}
	if spec != nil && spec.StrictReturn != nil {
		return *spec.StrictReturn
	}
	if reg, ok := registry.(verbRegistryStrictness); ok {
		if reg.StrictReturn() != nil {
			return *reg.StrictReturn()
		}
	}
	return false
}

func envBool(key string) (bool, bool) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return false, false
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func validateVerbArgs(spec *verb.Spec, args map[string]interface{}, registry VerbRegistry) error {
	if spec == nil {
		return nil
	}
	if !resolveStrictArgs(spec, registry) {
		return nil
	}

	required := spec.RequiredArgs
	if required == nil && len(spec.ArgTypes) > 0 {
		required = make([]string, 0, len(spec.ArgTypes))
		for name := range spec.ArgTypes {
			required = append(required, name)
		}
	}

	for _, name := range required {
		if _, ok := args[name]; !ok {
			return fmt.Errorf("missing required argument: %s", name)
		}
	}

	for name, value := range args {
		expectedTypeName, ok := spec.ArgTypes[name]
		if !ok {
			return fmt.Errorf("unexpected argument: %s", name)
		}
		expectedType, _ := types.ParseTypeName(expectedTypeName)
		actualType := types.InferTypeFromInterface(value)
		if expectedType != nil && !types.AreTypesCompatible(actualType, expectedType) {
			return fmt.Errorf("argument %s expected %s, got %s", name, expectedTypeName, actualType.String())
		}
	}

	return nil
}

func validateVerbReturn(spec *verb.Spec, result interface{}, registry VerbRegistry) error {
	if spec == nil {
		return nil
	}
	if !resolveStrictReturn(spec, registry) {
		return nil
	}

	expected := strings.TrimSpace(spec.ReturnType)
	if expected == "" || strings.EqualFold(expected, "unknown") || strings.EqualFold(expected, "any") {
		return nil
	}
	if strings.EqualFold(expected, "void") || strings.EqualFold(expected, "nil") {
		if result != nil {
			return fmt.Errorf("expected no return value for %s", spec.Name)
		}
		return nil
	}

	expectedType, _ := types.ParseTypeName(expected)
	actualType := types.InferTypeFromInterface(result)
	if expectedType != nil && !types.AreTypesCompatible(actualType, expectedType) {
		return fmt.Errorf("return value expected %s, got %s", expected, actualType.String())
	}

	return nil
}
