package types

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
)

// TypeSystem is the central type management system for Effectus
type TypeSystem struct {
	// Facts type registry - maps paths to types
	factTypes map[string]*Type
	// factVersions maps base paths to versioned types
	factVersions map[string]map[string]*Type
	// factDefaults tracks default version for a fact path
	factDefaults map[string]string

	// Verb specifications
	verbSpecs map[string]*VerbSpec

	// types is a map from type name to type
	types map[string]*Type

	// verbTypes maps verb names to their type information
	verbTypes map[string]*VerbInfo

	// functions stores expression function signatures
	functions map[string]*FunctionSpec

	// aliases maps namespace aliases to canonical prefixes
	aliases map[string]string

	// mutex for thread safety
	mu sync.RWMutex
}

// VerbSpec defines type requirements for a verb
type VerbSpec struct {
	Name         string           // Verb name
	ArgTypes     map[string]*Type // Types for each argument
	ReturnType   *Type            // Return type of the verb
	InverseVerb  string           // Optional inverse verb name for compensations
	RequiredArgs []string         // List of required arguments
	Capability   Capability       // Capability ID for locking
	Description  string           // Human-readable description
}

// VerbInfo contains type information for a verb
type VerbInfo struct {
	// ArgTypes maps argument names to their types
	ArgTypes map[string]*Type

	// ReturnType is the verb's return type
	ReturnType *Type
}

// NewTypeSystem creates a new type system
func NewTypeSystem() *TypeSystem {
	ts := &TypeSystem{
		factTypes:    make(map[string]*Type),
		factVersions: make(map[string]map[string]*Type),
		factDefaults: make(map[string]string),
		verbSpecs:    make(map[string]*VerbSpec),
		types:        make(map[string]*Type),
		verbTypes:    make(map[string]*VerbInfo),
		functions:    make(map[string]*FunctionSpec),
		aliases:      make(map[string]string),
	}
	ts.RegisterStandardLibrary()
	return ts
}

// RegisterType registers a named type in the system
func (ts *TypeSystem) RegisterType(name string, typ *Type) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Ensure the name is set and clone the type to avoid external modification
	cloned := typ.Clone()
	cloned.Name = name
	ts.types[name] = cloned
}

// GetType retrieves a type by name
func (ts *TypeSystem) GetType(name string) (*Type, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	typ, found := ts.types[name]
	if !found {
		return nil, false
	}

	// Return a clone to prevent external modification
	return typ.Clone(), true
}

// RegisterFactType registers a type for a fact path
func (ts *TypeSystem) RegisterFactType(path string, typ *Type) {
	ts.factTypes[path] = typ
}

// RegisterFactTypeVersion registers a versioned type for a fact path.
func (ts *TypeSystem) RegisterFactTypeVersion(path, version string, typ *Type, setDefault bool) {
	if path == "" || strings.TrimSpace(version) == "" || typ == nil {
		return
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.factVersions == nil {
		ts.factVersions = make(map[string]map[string]*Type)
	}
	if ts.factDefaults == nil {
		ts.factDefaults = make(map[string]string)
	}
	versions := ts.factVersions[path]
	if versions == nil {
		versions = make(map[string]*Type)
		ts.factVersions[path] = versions
	}
	versions[version] = typ
	if setDefault {
		ts.factDefaults[path] = version
		ts.factTypes[path] = typ
	}
}

// SetDefaultFactVersion sets the default version for a fact path.
func (ts *TypeSystem) SetDefaultFactVersion(path, version string) {
	if path == "" || strings.TrimSpace(version) == "" {
		return
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.factDefaults == nil {
		ts.factDefaults = make(map[string]string)
	}
	ts.factDefaults[path] = version
	if versions, ok := ts.factVersions[path]; ok {
		if typ, ok := versions[version]; ok {
			ts.factTypes[path] = typ
		}
	}
}

// GetFactTypeVersion retrieves a versioned fact type.
func (ts *TypeSystem) GetFactTypeVersion(path, version string) (*Type, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	if ts.factVersions == nil {
		return nil, fmt.Errorf("no versions registered for path: %s", path)
	}
	versions, ok := ts.factVersions[path]
	if !ok {
		return nil, fmt.Errorf("no versions registered for path: %s", path)
	}
	typ, ok := versions[version]
	if !ok {
		return nil, fmt.Errorf("no type registered for path %s version %s", path, version)
	}
	return typ, nil
}

// GetFactType retrieves the type for a fact path
func (ts *TypeSystem) GetFactType(path string) (*Type, error) {
	resolved := resolveAliasPath(path, ts.aliases)
	if base, version, ok := splitVersionedPath(resolved); ok {
		return ts.GetFactTypeVersion(base, version)
	}

	if ts.factDefaults != nil {
		if version, ok := ts.factDefaults[resolved]; ok {
			if typ, err := ts.GetFactTypeVersion(resolved, version); err == nil {
				return typ, nil
			}
		}
	}

	typ, exists := ts.factTypes[resolved]
	if !exists {
		return nil, fmt.Errorf("no type registered for path: %s", path)
	}
	return typ, nil
}

// GetAllFactPaths returns all registered fact paths
func (ts *TypeSystem) GetAllFactPaths() []string {
	paths := make(map[string]struct{})
	for path := range ts.factTypes {
		paths[path] = struct{}{}
	}
	for path, versions := range ts.factVersions {
		if _, ok := paths[path]; !ok {
			paths[path] = struct{}{}
		}
		for version := range versions {
			paths[path+"@"+version] = struct{}{}
		}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	return result
}

// GetAllVerbNames returns all registered verb names.
func (ts *TypeSystem) GetAllVerbNames() []string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	names := make([]string, 0, len(ts.verbSpecs))
	for name := range ts.verbSpecs {
		names = append(names, name)
	}
	return names
}

func splitVersionedPath(path string) (string, string, bool) {
	idx := strings.LastIndex(path, "@")
	if idx == -1 {
		return "", "", false
	}
	base := strings.TrimSpace(path[:idx])
	version := strings.TrimSpace(path[idx+1:])
	if base == "" || version == "" {
		return "", "", false
	}
	return base, version, true
}

// RegisterVerb registers a verb with full control over which arguments are required
// This is the most flexible registration method
func (ts *TypeSystem) RegisterVerb(verbName string, argTypes map[string]*Type, returnType *Type, requiredArgs []string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Clone all types to avoid external modification
	clonedArgTypes := make(map[string]*Type, len(argTypes))
	for name, typ := range argTypes {
		clonedArgTypes[name] = typ.Clone()
	}

	clonedReturnType := returnType.Clone()

	// Store verb type information in verbTypes
	ts.verbTypes[verbName] = &VerbInfo{
		ArgTypes:   clonedArgTypes,
		ReturnType: clonedReturnType,
	}

	// Validate required args are in the argTypes map
	for _, reqArg := range requiredArgs {
		if _, exists := clonedArgTypes[reqArg]; !exists {
			return fmt.Errorf("required argument %s is not defined in argTypes", reqArg)
		}
	}

	// Create a corresponding VerbSpec
	ts.verbSpecs[verbName] = &VerbSpec{
		Name:         verbName,
		ArgTypes:     clonedArgTypes,
		ReturnType:   clonedReturnType,
		RequiredArgs: requiredArgs,
		Description:  "Registered with RegisterVerb",
	}

	return nil
}

// GetVerbSpec retrieves a verb specification
func (ts *TypeSystem) GetVerbSpec(verbName string) (*VerbSpec, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	spec, exists := ts.verbSpecs[verbName]
	if !exists {
		return nil, fmt.Errorf("no specification for verb: %s", verbName)
	}
	return spec, nil
}

// LoadVerbSpecs loads verb specifications from a JSON file
func (ts *TypeSystem) LoadVerbSpecs(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read verb spec file: %w", err)
	}

	var specs map[string]*VerbSpec
	if err := json.Unmarshal(data, &specs); err != nil {
		return fmt.Errorf("failed to parse verb specs: %w", err)
	}

	// Merge with existing specs and populate verbTypes for flow checks.
	for name, spec := range specs {
		ts.verbSpecs[name] = spec
		if spec == nil {
			continue
		}
		argTypes := make(map[string]*Type, len(spec.ArgTypes))
		for argName, argType := range spec.ArgTypes {
			if argType == nil {
				argTypes[argName] = nil
				continue
			}
			argTypes[argName] = argType.Clone()
		}
		var returnType *Type
		if spec.ReturnType != nil {
			returnType = spec.ReturnType.Clone()
		}
		ts.verbTypes[name] = &VerbInfo{
			ArgTypes:   argTypes,
			ReturnType: returnType,
		}
	}

	return nil
}

// LoadSchemaFile loads fact types from a schema file
func (ts *TypeSystem) LoadSchemaFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("reading schema file: %w", err)
	}

	var schemaEntries []struct {
		Path string `json:"path"`
		Type Type   `json:"type"`
	}

	if err := json.Unmarshal(data, &schemaEntries); err != nil {
		return fmt.Errorf("parsing schema file: %w", err)
	}

	for _, entry := range schemaEntries {
		ts.RegisterFactType(entry.Path, &entry.Type)
	}

	return nil
}

// MergeTypeSystem merges another type system into this one
func (ts *TypeSystem) MergeTypeSystem(other *TypeSystem) {
	// Merge fact types
	for path, typ := range other.factTypes {
		ts.factTypes[path] = typ
	}
	for path, versions := range other.factVersions {
		if ts.factVersions == nil {
			ts.factVersions = make(map[string]map[string]*Type)
		}
		target := ts.factVersions[path]
		if target == nil {
			target = make(map[string]*Type)
			ts.factVersions[path] = target
		}
		for version, typ := range versions {
			target[version] = typ
		}
	}
	for path, version := range other.factDefaults {
		if ts.factDefaults == nil {
			ts.factDefaults = make(map[string]string)
		}
		ts.factDefaults[path] = version
	}

	// Merge verb specs
	for name, spec := range other.verbSpecs {
		ts.verbSpecs[name] = spec
	}
	// Merge verb type info (needed for flow step type checks).
	for name, info := range other.verbTypes {
		if info == nil {
			continue
		}
		cloned := &VerbInfo{
			ArgTypes: make(map[string]*Type, len(info.ArgTypes)),
		}
		for argName, argType := range info.ArgTypes {
			if argType == nil {
				cloned.ArgTypes[argName] = nil
				continue
			}
			cloned.ArgTypes[argName] = argType.Clone()
		}
		if info.ReturnType != nil {
			cloned.ReturnType = info.ReturnType.Clone()
		}
		ts.verbTypes[name] = cloned
	}

	// Merge function specs
	for name, spec := range other.functions {
		ts.functions[name] = spec
	}
}

// GetVerbTypes returns all verb type information
func (ts *TypeSystem) GetVerbTypes() map[string]*VerbInfo {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make(map[string]*VerbInfo, len(ts.verbTypes))
	for name, info := range ts.verbTypes {
		clonedInfo := &VerbInfo{
			ArgTypes:   make(map[string]*Type, len(info.ArgTypes)),
			ReturnType: info.ReturnType.Clone(),
		}

		for argName, argType := range info.ArgTypes {
			clonedInfo.ArgTypes[argName] = argType.Clone()
		}

		result[name] = clonedInfo
	}

	return result
}

// GetVerbType returns type information for a specific verb
func (ts *TypeSystem) GetVerbType(verbName string) (*VerbInfo, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	info, found := ts.verbTypes[verbName]
	if !found {
		return nil, false
	}

	// Return a copy to prevent external modification
	clonedInfo := &VerbInfo{
		ArgTypes:   make(map[string]*Type, len(info.ArgTypes)),
		ReturnType: info.ReturnType.Clone(),
	}

	for argName, argType := range info.ArgTypes {
		clonedInfo.ArgTypes[argName] = argType.Clone()
	}

	return clonedInfo, true
}

// TypeCheckPredicateAST checks a predicate in the AST
// This function is now replaced by the implementation in expr_adapter.go
// The new implementation handles string expressions directly

// GetAllTypes returns all registered named types
func (ts *TypeSystem) GetAllTypes() map[string]*Type {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make(map[string]*Type, len(ts.types))
	for name, typ := range ts.types {
		result[name] = typ.Clone()
	}
	return result
}

// TypeCheckPayload validates a payload against an expected type
func (ts *TypeSystem) TypeCheckPayload(payload interface{}, expectedType *Type) error {
	// Infer the actual type of the payload
	actualType := InferTypeFromValue(payload)

	// Check compatibility
	if !AreTypesCompatible(actualType, expectedType) {
		return fmt.Errorf("incompatible payload type: expected %s, got %s",
			expectedType.String(), actualType.String())
	}

	return nil
}

// typeCheckPredicate uses TypeCheckPredicateAST but adds fact checking
// This function is now replaced by the implementation in expr_adapter.go

// typeCheckEffect checks an effect for type correctness
func (ts *TypeSystem) typeCheckEffect(effect *ast.Effect, facts effectus.Facts) error {
	// This is a helper function we define here that uses operator_checks.go's functions
	verbSpec, err := ts.GetVerbSpec(effect.Verb)
	if err != nil {
		return fmt.Errorf("unknown verb: %s", effect.Verb)
	}

	// Check arguments
	for _, arg := range effect.Args {
		requiredType, exists := verbSpec.ArgTypes[arg.Name]
		if !exists {
			return fmt.Errorf("verb %s does not accept argument: %s", effect.Verb, arg.Name)
		}

		// Use TypeCheckArgValue from operator_checks.go
		if err := ts.TypeCheckArgValue(arg.Value, requiredType, facts); err != nil {
			return fmt.Errorf("argument %s: %w", arg.Name, err)
		}
	}

	// Check required arguments
	for _, requiredArg := range verbSpec.RequiredArgs {
		found := false
		for _, arg := range effect.Args {
			if arg.Name == requiredArg {
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("missing required argument %s for verb %s", requiredArg, effect.Verb)
		}
	}

	return nil
}

// typeCheckStep checks a flow step for type correctness
func (ts *TypeSystem) typeCheckStep(step *ast.Step, facts effectus.Facts) error {
	// Similar to typeCheckEffect but for flow steps
	verbInfo, found := ts.GetVerbType(step.Verb)
	if !found {
		return fmt.Errorf("undefined verb: %s", step.Verb)
	}

	// Check each argument
	for _, arg := range step.Args {
		if arg.Value == nil {
			return fmt.Errorf("missing value for argument %s", arg.Name)
		}

		// Check if argument is expected for this verb
		expectedType, exists := verbInfo.ArgTypes[arg.Name]
		if !exists {
			return fmt.Errorf("verb %s does not accept argument %s", step.Verb, arg.Name)
		}

		// Type check the argument value
		if err := ts.typeCheckArgValue(arg.Value, expectedType, facts); err != nil {
			return fmt.Errorf("invalid type for argument %s: %w", arg.Name, err)
		}
	}

	// Check for required arguments (simplified)
	for argName := range verbInfo.ArgTypes {
		found := false
		for _, arg := range step.Args {
			if arg.Name == argName {
				found = true
				break
			}
		}
		if !found {
			// This is simplified - in reality, not all arguments may be required
			return fmt.Errorf("missing required argument %s for verb %s", argName, step.Verb)
		}
	}

	return nil
}

// typeCheckArgValue checks if an argument value has compatible type with expected type
func (ts *TypeSystem) typeCheckArgValue(arg *ast.ArgValue, expectedType *Type, facts effectus.Facts) error {
	// Use the existing TypeCheckArgValue instead of duplicating
	return ts.TypeCheckArgValue(arg, expectedType, facts)
}

// BuildTypeSchemaFromFacts builds a type schema from facts
func (ts *TypeSystem) BuildTypeSchemaFromFacts(facts effectus.Facts) {
	// Get schema info
	schema := facts.Schema()
	_ = schema // Suppress unused variable warning

	// Simple implementation - real implementation would try to extract more info
	// from the facts and schema structure
}

// RegisterProtoTypes registers types from protobuf files
func (ts *TypeSystem) RegisterProtoTypes(protoFile string) error {
	// This is a simplified implementation
	// In a real implementation, it would parse proto files and register types
	return nil
}

// GenerateTypeReport generates a human-readable report of types
func (ts *TypeSystem) GenerateTypeReport() string {
	report := "# Type System Report\n\n"

	// Fact types
	report += "## Registered Fact Types\n\n"
	paths := ts.GetAllFactPaths()
	for _, path := range paths {
		typ, _ := ts.GetFactType(path)
		report += fmt.Sprintf("- `%s`: %s\n", path, typ)
	}

	// Verb specs
	report += "\n## Registered Verbs\n\n"
	for name, spec := range ts.verbSpecs {
		report += fmt.Sprintf("### %s\n", name)
		report += fmt.Sprintf("- Capability: %s\n", capabilityToString(spec.Capability))
		if spec.InverseVerb != "" {
			report += fmt.Sprintf("- Inverse: %s\n", spec.InverseVerb)
		}
		report += "- Arguments:\n"
		for argName, argType := range spec.ArgTypes {
			required := " "
			for _, req := range spec.RequiredArgs {
				if req == argName {
					required = "*"
					break
				}
			}
			report += fmt.Sprintf("  - %s%s: %s\n", required, argName, argType)
		}
		report += fmt.Sprintf("- Return: %s\n", spec.ReturnType)
	}

	return report
}

// Helper function to convert Capability to string
func capabilityToString(c Capability) string {
	switch c {
	case CapabilityNone:
		return "none"
	case CapabilityRead:
		return "read"
	case CapabilityModify:
		return "modify"
	case CapabilityCreate:
		return "create"
	case CapabilityDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// TypeCheckValue checks if a value matches the expected type
func (ts *TypeSystem) TypeCheckValue(value interface{}, expectedType *Type) error {
	actualType := InferTypeFromInterface(value)
	if !AreTypesCompatible(actualType, expectedType) {
		return fmt.Errorf("type mismatch: value %v of type %s is not compatible with expected type %s",
			value, actualType.String(), expectedType.String())
	}
	return nil
}

// TypeCheckFact checks if a fact at the given path has the expected type
func (ts *TypeSystem) TypeCheckFact(facts effectus.Facts, path string, expectedType *Type) error {
	value, exists := facts.Get(path)
	if !exists {
		return fmt.Errorf("fact path does not exist: %s", path)
	}

	return ts.TypeCheckValue(value, expectedType)
}

// InferTypeFromFactPath tries to infer a type from a fact if no type is registered
func (ts *TypeSystem) InferTypeFromFactPath(facts effectus.Facts, path string) (*Type, error) {
	// First, check if a type is already registered
	existingType, err := ts.GetFactType(path)
	if err == nil && existingType != nil {
		return existingType, nil
	}

	// Try to infer from actual value
	value, exists := facts.Get(path)
	if !exists {
		return nil, fmt.Errorf("fact path does not exist: %s", path)
	}

	inferredType := InferTypeFromInterface(value)
	// Register the inferred type
	ts.RegisterFactType(path, inferredType)

	return inferredType, nil
}

// AutoRegisterTypes automatically registers types for all facts
func (ts *TypeSystem) AutoRegisterTypes(facts effectus.Facts) {
	// This would need to iterate over all available facts
	// For now, we'll just log that it's not fully implemented
	fmt.Println("Warning: AutoRegisterTypes is not fully implemented")
}

// TypeCheckLogicalExpressionAST checks a logical expression in AST
// This function is now replaced by the implementation in expr_adapter.go
// The new implementation handles string expressions directly

// TypeCheckArgumentValue type checks an argument value against the required type
func (ts *TypeSystem) TypeCheckArgumentValue(arg *ast.ArgValue, requiredType *Type, facts Facts) error {
	if arg == nil {
		return fmt.Errorf("argument value is nil")
	}

	// Handle different value sources
	if arg.Literal != nil {
		// For literals, directly check against required type
		valueType := InferTypeFromInterface(arg.Literal)
		if !AreTypesCompatible(valueType, requiredType) {
			return fmt.Errorf("literal value type %s not compatible with required type %s",
				valueType.String(), requiredType.String())
		}
		return nil
	}

	if arg.PathExpr != nil {
		// For path expressions, first get the fact type, then check compatibility
		factType, err := ts.GetFactType(arg.PathExpr.Path)
		if err != nil {
			return fmt.Errorf("unknown fact path: %s", arg.PathExpr.Path)
		}

		if !AreTypesCompatible(factType, requiredType) {
			return fmt.Errorf("fact path %s has type %s which is not compatible with required type %s",
				arg.PathExpr.Path, factType.String(), requiredType.String())
		}
		return nil
	}

	if arg.VarRef != "" {
		// Variable references would be checked against a symbol table
		// For now we'll just show a placeholder implementation
		return fmt.Errorf("variable reference type checking not fully implemented")
	}

	return fmt.Errorf("invalid argument value: neither literal, path, nor variable reference")
}

// CanAssign checks if a value of sourceType can be assigned to a variable of targetType
func CanAssign(sourceType, targetType *Type) bool {
	// Direct compatibility
	if AreTypesCompatible(sourceType, targetType) {
		return true
	}

	// Special cases for numeric conversions with potential narrowing
	if sourceType.PrimType == TypeInt && targetType.PrimType == TypeFloat {
		// Int can always be converted to float without loss
		return true
	}

	if sourceType.PrimType == TypeFloat && targetType.PrimType == TypeInt {
		// Float to int is possible with potential truncation
		// In strict mode this might return false
		return true
	}

	// Special case for lists
	if sourceType.PrimType == TypeList && targetType.PrimType == TypeList {
		// Empty source list can be assigned to any list type
		if sourceType.ElementType == nil || sourceType.ElementType.PrimType == TypeUnknown {
			return true
		}

		// Otherwise elements must be compatible
		return CanAssign(sourceType.ElementType, targetType.ElementType)
	}

	// Special case for maps
	if sourceType.PrimType == TypeMap && targetType.PrimType == TypeMap {
		// Empty source map can be assigned to any map type
		if sourceType.MapKeyType() == nil || sourceType.MapValueType() == nil {
			return true
		}

		// Otherwise both key and value types must be compatible
		return CanAssign(sourceType.MapKeyType(), targetType.MapKeyType()) &&
			CanAssign(sourceType.MapValueType(), targetType.MapValueType())
	}

	return false
}

// IsCoercibleTo checks if a value can be coerced to another type
func IsCoercibleTo(sourceType, targetType *Type) bool {
	// Direct assignment always works
	if CanAssign(sourceType, targetType) {
		return true
	}

	// String coercions
	if targetType.PrimType == TypeString {
		// Almost anything can be converted to string
		return sourceType.PrimType != TypeUnknown
	}

	if sourceType.PrimType == TypeString {
		// String to number conversions
		if targetType.PrimType == TypeInt || targetType.PrimType == TypeFloat {
			return true // With proper validation at runtime
		}

		// String to boolean conversion
		if targetType.PrimType == TypeBool {
			return true // With proper validation at runtime
		}

		// String to time conversions
		if targetType.PrimType == TypeTime || targetType.PrimType == TypeDate {
			return true // With proper validation at runtime
		}
	}

	return false
}

// TypeCheckFile performs type checking on a parsed file
func (ts *TypeSystem) TypeCheckFile(file *ast.File, facts effectus.Facts) error {
	// Type check rules
	for _, rule := range file.Rules {
		if err := ts.typeCheckRule(rule, facts); err != nil {
			return fmt.Errorf("type error in rule %s: %w", rule.Name, err)
		}
	}

	// Type check flows
	for _, flow := range file.Flows {
		if err := ts.typeCheckFlow(flow, facts); err != nil {
			return fmt.Errorf("type error in flow %s: %w", flow.Name, err)
		}
	}

	return nil
}

// typeCheckRule performs type checking on a rule
func (ts *TypeSystem) typeCheckRule(rule *ast.Rule, facts effectus.Facts) error {
	// Type check predicates in each when-then block using the helper from expr_adapter.go
	if err := ts.typeCheckRulePredicates(rule, facts); err != nil {
		return err
	}

	// Type check effects
	for _, block := range rule.Blocks {
		if block.Then != nil {
			for _, effect := range block.Then.Effects {
				if err := ts.typeCheckEffect(effect, facts); err != nil {
					return fmt.Errorf("effect error (%s): %w", effect.Verb, err)
				}
			}
		}
	}

	return nil
}

// typeCheckFlow performs type checking on a flow
func (ts *TypeSystem) typeCheckFlow(flow *ast.Flow, facts effectus.Facts) error {
	// Type check predicates using the helper from expr_adapter.go
	if err := ts.typeCheckFlowPredicates(flow, facts); err != nil {
		return err
	}

	// Type check steps
	if flow.Steps != nil {
		for _, step := range flow.Steps.Steps {
			if err := ts.typeCheckStep(step, facts); err != nil {
				return fmt.Errorf("step error (%s): %w", step.Verb, err)
			}
		}
	}

	return nil
}

// typeCheckLogicalExpression type checks a logical expression
// This function is now replaced by the implementation in expr_adapter.go

// RegisterVerbType registers a verb with its argument types and return type
// All arguments are considered required by default
func (ts *TypeSystem) RegisterVerbType(verbName string, argTypes map[string]*Type, returnType *Type) error {
	// Create a list of all arguments as required
	requiredArgs := make([]string, 0, len(argTypes))
	for argName := range argTypes {
		requiredArgs = append(requiredArgs, argName)
	}

	// Use the more general RegisterVerb method
	return ts.RegisterVerb(verbName, argTypes, returnType, requiredArgs)
}
