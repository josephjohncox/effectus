package compiler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/effectus/effectus-go/ast"
	effectusv1 "github.com/effectus/effectus-go/gen/effectus/v1"
	"github.com/effectus/effectus-go/ir"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
	exprast "github.com/expr-lang/expr/ast"
	exprparser "github.com/expr-lang/expr/parser"
)

// Source is one in-memory Effectus source file. Path determines the source
// dialect and canonical declaration order; Content is never read again after
// CompileChecked returns.
type Source struct {
	Path    string
	Content []byte
	Data    []byte // Deprecated: use Content.
}

// CompileOptions controls properties that must be frozen into checked IR.
type CompileOptions struct {
	ExecutionPolicy effectusv1.ExecutionPolicy
	Limits          ir.Limits
}

const (
	ExecutionPolicyFailFast     = effectusv1.ExecutionPolicy_EXECUTION_POLICY_DURABLE_FAIL_FAST
	ExecutionPolicyCompensating = effectusv1.ExecutionPolicy_EXECUTION_POLICY_DURABLE_COMPENSATING
)

// LoadSources reads Effectus source paths for CompileChecked. It is the shared
// file front end used by command-line checked compilation.
func LoadSources(paths []string) ([]Source, error) {
	sources := make([]Source, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read source %s: %w", path, err)
		}
		sources = append(sources, Source{Path: path, Content: data})
	}
	return sources, nil
}

// BuildIREnvironment converts loaded schema and verb declarations to the
// immutable declaration environment required by checked compilation.
func BuildIREnvironment(typeSystem *types.TypeSystem, registry *verb.Registry) (ir.Environment, error) {
	environment := ir.Environment{
		Facts: make(map[string]string), Verbs: make(map[string]ir.VerbContract),
		Functions: make(map[string]ir.FunctionContract), Types: make(map[string]ir.TypeDefinition),
	}
	if typeSystem != nil {
		for name, typ := range typeSystem.GetAllTypes() {
			definition, structural, err := irTypeDefinition(typ)
			if err != nil {
				return ir.Environment{}, fmt.Errorf("type %s: %w", name, err)
			}
			if structural {
				environment.Types[name] = definition
			}
		}
		for _, path := range typeSystem.GetAllFactPaths() {
			typ, err := typeSystem.GetFactType(path)
			if err != nil {
				return ir.Environment{}, fmt.Errorf("fact %s: %w", path, err)
			}
			environment.Facts[path], err = irTypeName(typ)
			if err != nil {
				return ir.Environment{}, fmt.Errorf("fact %s: %w", path, err)
			}
		}
		for _, spec := range types.StandardLibrary() {
			if spec == nil || spec.Unsafe {
				continue
			}
			contract := ir.FunctionContract{
				ArgumentTypes: append([]string(nil), spec.ArgTypes...), ReturnType: spec.ReturnType, Pure: true, Total: true,
			}
			// Polymorphic legacy helpers use open "any" contracts, which checked IR
			// intentionally cannot prove. Do not advertise them as checked functions.
			probe := ir.Environment{Types: environment.Types, Functions: map[string]ir.FunctionContract{spec.Name: contract}}
			if _, err := ir.EnvironmentDigest(probe); err == nil {
				environment.Functions[spec.Name] = contract
			}
		}
	}
	if registry != nil {
		for _, spec := range registry.GetAllVerbs() {
			contract := ir.VerbContract{
				Arguments: cloneStringValues(spec.ArgTypes), RequiredArgs: append([]string(nil), spec.RequiredArgs...),
				ResultType: spec.ReturnType, InverseVerb: spec.Inverse, RetryPolicy: ir.RetryPolicy{MaxAttempts: 1},
			}
			if spec.Capability&verb.CapIdempotent != 0 {
				contract.IdempotencyPolicy = ir.IdempotencyKeyRequired
			}
			contract.FencingRequired = spec.Capability&verb.CapExclusive != 0
			environment.Verbs[spec.Name] = contract
		}
	}
	if _, err := ir.EnvironmentDigest(environment); err != nil {
		return ir.Environment{}, fmt.Errorf("checked declarations: %w", err)
	}
	return environment, nil
}

func irTypeName(typ *types.Type) (string, error) {
	if typ == nil {
		return "", fmt.Errorf("type is nil")
	}
	if typ.ReferenceType != "" {
		return typ.ReferenceType, nil
	}
	switch typ.PrimType {
	case types.TypeBool:
		return "bool", nil
	case types.TypeInt:
		return "int", nil
	case types.TypeFloat:
		return "float", nil
	case types.TypeString:
		return "string", nil
	case types.TypeTime, types.TypeDate, types.TypeDuration:
		return "", fmt.Errorf("temporal type %s is not supported by checked IR", typ.String())
	case types.TypeList:
		element, err := irTypeName(typ.ElementType)
		return "[]" + element, err
	case types.TypeMap:
		element, err := irTypeName(typ.MapValueType())
		return "map<" + element + ">", err
	case types.TypeObject:
		if typ.Name != "" {
			return typ.Name, nil
		}
	}
	if typ.Name != "" {
		return typ.Name, nil
	}
	return "", fmt.Errorf("open or unknown type is not valid in checked IR")
}

func irTypeDefinition(typ *types.Type) (ir.TypeDefinition, bool, error) {
	if typ == nil {
		return ir.TypeDefinition{}, false, fmt.Errorf("type is nil")
	}
	switch typ.PrimType {
	case types.TypeObject:
		fields := make(map[string]string, len(typ.Properties))
		for name, fieldType := range typ.Properties {
			converted, err := irTypeName(fieldType)
			if err != nil {
				return ir.TypeDefinition{}, false, fmt.Errorf("field %s: %w", name, err)
			}
			fields[name] = converted
		}
		return ir.TypeDefinition{Kind: ir.TypeKindObject, Fields: fields}, true, nil
	case types.TypeList:
		element, err := irTypeName(typ.ElementType)
		return ir.TypeDefinition{Kind: ir.TypeKindList, ElementType: element}, true, err
	case types.TypeMap:
		element, err := irTypeName(typ.MapValueType())
		return ir.TypeDefinition{Kind: ir.TypeKindMap, ElementType: element}, true, err
	default:
		return ir.TypeDefinition{}, false, nil
	}
}

const (
	checkedCompilerName    = "effectusc"
	checkedCompilerVersion = "checked-ir-v1"
)

// CompileChecked is the production compiler boundary. It parses .eff and
// .effx sources, lowers their source ASTs without creating executable legacy
// specs, and returns only an opaque value that has passed ir.Check.
func CompileChecked(ctx context.Context, sources []Source, environment ir.Environment, options CompileOptions) (*ir.Checked, error) {
	if ctx == nil {
		return nil, fmt.Errorf("compile checked: context is nil")
	}
	policy := options.ExecutionPolicy
	if policy == effectusv1.ExecutionPolicy_EXECUTION_POLICY_UNSPECIFIED {
		policy = ExecutionPolicyFailFast
	}
	if policy != ExecutionPolicyFailFast && policy != ExecutionPolicyCompensating {
		return nil, fmt.Errorf("compile checked: unsupported execution policy %s", policy)
	}

	parser, err := participle.Build[ast.File](
		participle.Lexer(ast.Lexer),
		participle.UseLookahead(2),
		participle.Elide("Whitespace", "Comment"),
	)
	if err != nil {
		return nil, fmt.Errorf("compile checked: build parser: %w", err)
	}

	type parsed struct {
		path string
		file *ast.File
	}
	ordered := make([]parsed, 0, len(sources))
	seenPaths := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, err := normalizeSourcePath(source.Path)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seenPaths[path]; duplicate {
			return nil, fmt.Errorf("compile checked: duplicate normalized source path %q", path)
		}
		seenPaths[path] = struct{}{}
		extension := filepath.Ext(path)
		if extension != ".eff" && extension != ".effx" {
			return nil, fmt.Errorf("compile checked: source %q must use .eff or .effx", path)
		}
		data, err := checkedSourceBytes(source)
		if err != nil {
			return nil, err
		}
		file, err := parser.ParseBytes(path, data)
		if err != nil {
			return nil, fmt.Errorf("compile checked: parse %s: %w", path, err)
		}
		if err := restoreCheckedPredicateText(file, data); err != nil {
			return nil, fmt.Errorf("compile checked: %s: %w", path, err)
		}
		normalizeCheckedSourceAST(file)
		if err := normalizeFlowBindings(file); err != nil {
			return nil, fmt.Errorf("compile checked: %s: %w", path, err)
		}
		if extension == ".eff" && len(file.Flows) != 0 {
			return nil, fmt.Errorf("compile checked: %s contains flow declarations", path)
		}
		if extension == ".effx" && len(file.Rules) != 0 {
			return nil, fmt.Errorf("compile checked: %s contains list rule declarations", path)
		}
		ordered = append(ordered, parsed{path: path, file: file})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].path < ordered[j].path })

	plans := make([]*effectusv1.Plan, 0)
	var listOrder, flowOrder uint32
	for _, source := range ordered {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		switch filepath.Ext(source.path) {
		case ".eff":
			rules := append([]*ast.Rule(nil), source.file.Rules...)
			sort.SliceStable(rules, func(i, j int) bool {
				return declarationBefore(rules[i].Pos.Line, rules[i].Pos.Column, rules[j].Pos.Line, rules[j].Pos.Column)
			})
			for _, rule := range rules {
				plan, err := lowerListRule(rule, listOrder, environment, policy)
				if err != nil {
					return nil, fmt.Errorf("compile checked: %s:%d:%d: %w", source.path, rule.Pos.Line, rule.Pos.Column, err)
				}
				plans = append(plans, plan)
				listOrder++
			}
		case ".effx":
			flows := append([]*ast.Flow(nil), source.file.Flows...)
			sort.SliceStable(flows, func(i, j int) bool {
				return declarationBefore(flows[i].Pos.Line, flows[i].Pos.Column, flows[j].Pos.Line, flows[j].Pos.Column)
			})
			for _, flow := range flows {
				plan, err := lowerFlow(flow, flowOrder, environment, policy)
				if err != nil {
					return nil, fmt.Errorf("compile checked: %s:%d:%d: %w", source.path, flow.Pos.Line, flow.Pos.Column, err)
				}
				plans = append(plans, plan)
				flowOrder++
			}
		}
	}
	ir.CanonicalPlanOrder(plans)
	environmentDigest, err := ir.EnvironmentDigest(environment)
	if err != nil {
		return nil, fmt.Errorf("compile checked: environment: %w", err)
	}
	build := sha256.Sum256([]byte(checkedCompilerName + "\x00" + checkedCompilerVersion))
	checked, err := ir.Check(&effectusv1.RuleArtifact{
		FormatVersion: ir.FormatVersion,
		Compiler: &effectusv1.CompilerMetadata{
			Name: checkedCompilerName, Version: checkedCompilerVersion, BuildDigest: hex.EncodeToString(build[:]),
		},
		EnvironmentDigest: environmentDigest,
		Plans:             plans,
	}, environment, options.Limits)
	if err != nil {
		return nil, fmt.Errorf("compile checked: validate lowered artifact: %w", err)
	}
	return checked, nil
}

// CompileChecked provides a method facade for callers that already own a Compiler.
func (c *Compiler) CompileChecked(ctx context.Context, sources []Source, environment ir.Environment, options CompileOptions) (*ir.Checked, error) {
	return CompileChecked(ctx, sources, environment, options)
}

func checkedSourceBytes(source Source) ([]byte, error) {
	if source.Content != nil && source.Data != nil && !bytes.Equal(source.Content, source.Data) {
		return nil, fmt.Errorf("compile checked: source %q supplies conflicting Content and deprecated Data bytes", source.Path)
	}
	if source.Content != nil {
		return source.Content, nil
	}
	return source.Data, nil
}

func restoreCheckedPredicateText(file *ast.File, data []byte) error {
	expressions, err := extractWhenBlocks(data)
	if err != nil {
		return err
	}
	index := 0
	for _, rule := range file.Rules {
		if rule == nil {
			continue
		}
		for _, pair := range rule.Blocks {
			if pair == nil {
				continue
			}
			if index >= len(expressions) {
				return fmt.Errorf("parsed AST contains more when blocks than source")
			}
			if pair.When == nil {
				pair.When = &ast.PredicateBlock{Pos: pair.Pos}
			}
			pair.When.Expression = strings.TrimSpace(expressions[index])
			index++
		}
	}
	for _, flow := range file.Flows {
		if flow == nil {
			continue
		}
		if index >= len(expressions) {
			return fmt.Errorf("parsed AST contains more when blocks than source")
		}
		if flow.When == nil {
			flow.When = &ast.PredicateBlock{Pos: flow.Pos}
		}
		flow.When.Expression = strings.TrimSpace(expressions[index])
		index++
	}
	if index != len(expressions) {
		return fmt.Errorf("found %d when blocks in source and %d in parsed AST", len(expressions), index)
	}
	return nil
}

func extractWhenBlocks(data []byte) ([]string, error) {
	var expressions []string
	for index := 0; index < len(data); {
		if data[index] == '"' || data[index] == '\'' {
			index = skipQuotedSource(data, index)
			continue
		}
		if data[index] == '/' && index+1 < len(data) && data[index+1] == '/' || data[index] == '#' {
			index = skipSourceComment(data, index)
			continue
		}
		if !isSourceIdentStart(data[index]) {
			index++
			continue
		}
		start := index
		for index < len(data) && isSourceIdentPart(data[index]) {
			index++
		}
		if string(data[start:index]) != "when" {
			continue
		}
		open := skipSourceTrivia(data, index)
		if open >= len(data) || data[open] != '{' {
			continue
		}
		depth := 1
		bodyStart := open + 1
		cursor := bodyStart
		for cursor < len(data) && depth > 0 {
			switch {
			case data[cursor] == '"' || data[cursor] == '\'':
				cursor = skipQuotedSource(data, cursor)
			case data[cursor] == '/' && cursor+1 < len(data) && data[cursor+1] == '/' || data[cursor] == '#':
				cursor = skipSourceComment(data, cursor)
			case data[cursor] == '{':
				depth++
				cursor++
			case data[cursor] == '}':
				depth--
				if depth == 0 {
					expressions = append(expressions, string(data[bodyStart:cursor]))
					index = cursor + 1
				} else {
					cursor++
				}
			default:
				cursor++
			}
		}
		if depth != 0 {
			return nil, fmt.Errorf("unterminated when block")
		}
	}
	return expressions, nil
}

func skipQuotedSource(data []byte, index int) int {
	quote := data[index]
	index++
	for index < len(data) {
		if data[index] == '\\' {
			index += 2
			continue
		}
		index++
		if data[index-1] == quote {
			break
		}
	}
	return index
}

func skipSourceComment(data []byte, index int) int {
	for index < len(data) && data[index] != '\n' {
		index++
	}
	return index
}

func skipSourceTrivia(data []byte, index int) int {
	for index < len(data) {
		switch {
		case data[index] == ' ' || data[index] == '\t' || data[index] == '\r' || data[index] == '\n':
			index++
		case data[index] == '/' && index+1 < len(data) && data[index+1] == '/':
			index = skipSourceComment(data, index)
		case data[index] == '#':
			index = skipSourceComment(data, index)
		default:
			return index
		}
	}
	return index
}

func isSourceIdentStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isSourceIdentPart(value byte) bool {
	return isSourceIdentStart(value) || value >= '0' && value <= '9'
}

func normalizeCheckedSourceAST(file *ast.File) {
	if file == nil {
		return
	}
	for _, rule := range file.Rules {
		if rule == nil {
			continue
		}
		rule.PostProcess()
		for _, block := range rule.Blocks {
			if block == nil {
				continue
			}
			if block.When != nil {
				block.When.PostProcess()
			}
			if block.Then != nil {
				for _, effect := range block.Then.Effects {
					normalizeInvocationLiterals(effect.Args)
				}
			}
		}
	}
	for _, flow := range file.Flows {
		if flow == nil {
			continue
		}
		flow.PostProcess()
		if flow.When != nil {
			flow.When.PostProcess()
		}
		if flow.Steps != nil {
			for _, step := range flow.Steps.Steps {
				if step != nil {
					normalizeInvocationLiterals(step.Args)
				}
			}
		}
	}
}

func normalizeInvocationLiterals(arguments []*ast.StepArg) {
	for _, argument := range arguments {
		if argument != nil && argument.Value != nil && argument.Value.Literal != nil {
			normalizeASTLiteral(argument.Value.Literal)
		}
	}
}

func normalizeASTLiteral(literal *ast.Literal) {
	if literal == nil {
		return
	}
	literal.PostProcess()
	for index := range literal.List {
		normalizeASTLiteral(&literal.List[index])
	}
	for _, entry := range literal.Map {
		if entry != nil {
			normalizeASTLiteral(&entry.Value)
		}
	}
}

func normalizeSourcePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("compile checked: source path is empty")
	}
	path = filepath.ToSlash(filepath.Clean(path))
	if path == "." || strings.HasPrefix(path, "../") || path == ".." {
		return "", fmt.Errorf("compile checked: source path %q escapes its source root", path)
	}
	return path, nil
}

func declarationBefore(lineA, columnA, lineB, columnB int) bool {
	if lineA != lineB {
		return lineA < lineB
	}
	return columnA < columnB
}

func lowerListRule(rule *ast.Rule, sourceOrder uint32, environment ir.Environment, policy effectusv1.ExecutionPolicy) (*effectusv1.Plan, error) {
	if rule == nil || strings.TrimSpace(rule.Name) == "" || rule.Name != strings.TrimSpace(rule.Name) {
		return nil, fmt.Errorf("invalid list rule name")
	}
	predicates := make([]*effectusv1.Expression, 0, len(rule.Blocks))
	effects := make([]*ast.Effect, 0)
	for _, block := range rule.Blocks {
		if block == nil {
			return nil, fmt.Errorf("rule %q contains a nil when/then block", rule.Name)
		}
		if block.When != nil && strings.TrimSpace(block.When.Expression) != "" {
			expression, err := lowerPredicate(block.When.Expression)
			if err != nil {
				return nil, fmt.Errorf("rule %q predicate: %w", rule.Name, err)
			}
			predicates = append(predicates, expression)
		}
		if block.Then != nil {
			effects = append(effects, block.Then.Effects...)
		}
	}
	steps, err := lowerInvocations(rule.Name, effectsToInvocations(effects), environment, policy)
	if err != nil {
		return nil, fmt.Errorf("rule %q: %w", rule.Name, err)
	}
	return &effectusv1.Plan{
		Id: rule.Name, SourceDialect: effectusv1.SourceDialect_SOURCE_DIALECT_LIST,
		SourceOrder: sourceOrder, Priority: int32(rule.Priority), Predicate: &effectusv1.Predicate{Expression: conjunction(predicates)},
		ExecutionPolicy: policy, Steps: steps,
	}, nil
}

func lowerFlow(flow *ast.Flow, sourceOrder uint32, environment ir.Environment, policy effectusv1.ExecutionPolicy) (*effectusv1.Plan, error) {
	if flow == nil || strings.TrimSpace(flow.Name) == "" || flow.Name != strings.TrimSpace(flow.Name) {
		return nil, fmt.Errorf("invalid flow name")
	}
	predicate := trueExpression()
	if flow.When != nil && strings.TrimSpace(flow.When.Expression) != "" {
		var err error
		predicate, err = lowerPredicate(flow.When.Expression)
		if err != nil {
			return nil, fmt.Errorf("flow %q predicate: %w", flow.Name, err)
		}
	}
	var invocations []sourceInvocation
	if flow.Steps != nil {
		invocations = make([]sourceInvocation, 0, len(flow.Steps.Steps))
		for _, step := range flow.Steps.Steps {
			if step == nil {
				return nil, fmt.Errorf("flow %q contains a nil step", flow.Name)
			}
			invocations = append(invocations, sourceInvocation{verb: step.Verb, args: step.Args, binding: step.BindName})
		}
	}
	steps, err := lowerInvocations(flow.Name, invocations, environment, policy)
	if err != nil {
		return nil, fmt.Errorf("flow %q: %w", flow.Name, err)
	}
	return &effectusv1.Plan{
		Id: flow.Name, SourceDialect: effectusv1.SourceDialect_SOURCE_DIALECT_FLOW,
		SourceOrder: sourceOrder, Priority: int32(flow.Priority), Predicate: &effectusv1.Predicate{Expression: predicate},
		ExecutionPolicy: policy, Steps: steps,
	}, nil
}

type sourceInvocation struct {
	verb    string
	args    []*ast.StepArg
	binding string
}

func effectsToInvocations(effects []*ast.Effect) []sourceInvocation {
	out := make([]sourceInvocation, 0, len(effects))
	for _, effect := range effects {
		if effect == nil {
			out = append(out, sourceInvocation{})
			continue
		}
		out = append(out, sourceInvocation{verb: effect.Verb, args: effect.Args, binding: effect.BindName})
	}
	return out
}

func lowerInvocations(planID string, invocations []sourceInvocation, environment ir.Environment, policy effectusv1.ExecutionPolicy) ([]*effectusv1.Step, error) {
	steps := make([]*effectusv1.Step, 0, len(invocations))
	bindings := make(map[string]uint32)
	for index, invocation := range invocations {
		if strings.TrimSpace(invocation.verb) == "" {
			return nil, fmt.Errorf("step %d has an empty verb", index+1)
		}
		contract, ok := environment.Verbs[invocation.verb]
		if !ok {
			return nil, fmt.Errorf("step %d references unknown verb %q", index+1, invocation.verb)
		}
		contractHash, err := ir.ContractHash(contract)
		if err != nil {
			return nil, fmt.Errorf("step %d verb %q contract: %w", index+1, invocation.verb, err)
		}
		arguments, err := lowerArguments(invocation.args, bindings)
		if err != nil {
			return nil, fmt.Errorf("step %d verb %q: %w", index+1, invocation.verb, err)
		}
		step := &effectusv1.Step{
			Id: fmt.Sprintf("%s.step.%06d", planID, index+1), Ordinal: uint32(index), Verb: invocation.verb,
			ContractHash: contractHash, Arguments: arguments,
		}
		freezeStepPolicies(step, contract, environment)
		if policy == ExecutionPolicyCompensating {
			if err := validateCompensationContract(invocation.verb, contract, environment); err != nil {
				return nil, fmt.Errorf("step %d: %w", index+1, err)
			}
		}
		if invocation.binding != "" {
			name := strings.TrimSpace(invocation.binding)
			if name == "" || name != invocation.binding {
				return nil, fmt.Errorf("step %d has an invalid result binding", index+1)
			}
			if _, duplicate := bindings[name]; duplicate {
				return nil, fmt.Errorf("step %d redefines result binding %q", index+1, name)
			}
			slot := uint32(len(bindings))
			bindings[name] = slot
			step.ResultSlot = &slot
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func freezeStepPolicies(step *effectusv1.Step, contract ir.VerbContract, environment ir.Environment) {
	maxAttempts := contract.RetryPolicy.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 1
	}
	step.RetryPolicy = &effectusv1.CheckedRetryPolicy{
		MaxAttempts: maxAttempts, InitialBackoffMillis: contract.RetryPolicy.InitialBackoffMillis, MaxBackoffMillis: contract.RetryPolicy.MaxBackoffMillis,
	}
	switch contract.IdempotencyPolicy {
	case ir.IdempotencyKeyRequired:
		step.IdempotencyPolicy = effectusv1.IdempotencyPolicy_IDEMPOTENCY_POLICY_KEY_REQUIRED
	case ir.IdempotencySinkGuaranteed:
		step.IdempotencyPolicy = effectusv1.IdempotencyPolicy_IDEMPOTENCY_POLICY_SINK_GUARANTEED
	default:
		step.IdempotencyPolicy = effectusv1.IdempotencyPolicy_IDEMPOTENCY_POLICY_NONE
	}
	if contract.FencingRequired {
		step.FencingRequirement = effectusv1.FencingRequirement_FENCING_REQUIREMENT_REQUIRED
	} else {
		step.FencingRequirement = effectusv1.FencingRequirement_FENCING_REQUIREMENT_NONE
	}
	if inverse, ok := environment.Verbs[contract.InverseVerb]; contract.InverseVerb != "" && ok {
		if hash, err := ir.ContractHash(inverse); err == nil {
			step.Compensation = &effectusv1.CompensationContract{InverseVerb: contract.InverseVerb, InverseContractHash: hash}
		}
	}
}

func validateCompensationContract(verb string, contract ir.VerbContract, environment ir.Environment) error {
	if contract.InverseVerb == "" {
		return fmt.Errorf("compensating execution requires verb %q to declare an inverse", verb)
	}
	inverse, ok := environment.Verbs[contract.InverseVerb]
	if !ok {
		return fmt.Errorf("verb %q references unknown inverse %q", verb, contract.InverseVerb)
	}
	if !equalStringMap(contract.Arguments, inverse.Arguments) || !equalStringSet(contract.RequiredArgs, inverse.RequiredArgs) {
		return fmt.Errorf("verb %q inverse %q has an incompatible argument contract", verb, contract.InverseVerb)
	}
	return nil
}

func equalStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func equalStringSet(left, right []string) bool {
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	sort.Strings(left)
	sort.Strings(right)
	return strings.Join(left, "\x00") == strings.Join(right, "\x00")
}

func lowerArguments(arguments []*ast.StepArg, bindings map[string]uint32) ([]*effectusv1.Argument, error) {
	byName := make(map[string]*ast.ArgValue, len(arguments))
	for index, argument := range arguments {
		if argument == nil || strings.TrimSpace(argument.Name) == "" {
			return nil, fmt.Errorf("argument %d is invalid", index+1)
		}
		if _, duplicate := byName[argument.Name]; duplicate {
			return nil, fmt.Errorf("duplicate argument %q", argument.Name)
		}
		byName[argument.Name] = argument.Value
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	lowered := make([]*effectusv1.Argument, 0, len(names))
	for _, name := range names {
		value, err := lowerArgumentValue(byName[name], bindings)
		if err != nil {
			return nil, fmt.Errorf("argument %q: %w", name, err)
		}
		lowered = append(lowered, &effectusv1.Argument{Name: name, Value: value})
	}
	return lowered, nil
}

func lowerArgumentValue(value *ast.ArgValue, bindings map[string]uint32) (*effectusv1.Value, error) {
	if value == nil {
		return nil, fmt.Errorf("value is nil")
	}
	kinds := 0
	if value.Literal != nil {
		kinds++
	}
	if value.PathExpr != nil {
		kinds++
	}
	if value.VarRef != "" {
		kinds++
	}
	if kinds != 1 {
		return nil, fmt.Errorf("value must contain exactly one literal, fact path, or result reference")
	}
	if value.Literal != nil {
		literal, err := lowerASTLiteral(value.Literal)
		if err != nil {
			return nil, err
		}
		return &effectusv1.Value{Kind: &effectusv1.Value_Literal{Literal: literal}}, nil
	}
	if value.PathExpr != nil {
		path := strings.TrimSpace(value.PathExpr.Path)
		if path == "" {
			return nil, fmt.Errorf("fact path is empty")
		}
		return &effectusv1.Value{Kind: &effectusv1.Value_FactPath{FactPath: path}}, nil
	}
	name := strings.TrimPrefix(value.VarRef, "$")
	if name == value.VarRef || name == "" {
		return nil, fmt.Errorf("invalid result reference %q", value.VarRef)
	}
	slot, ok := bindings[name]
	if !ok {
		return nil, fmt.Errorf("result binding %q is not available", name)
	}
	return &effectusv1.Value{Kind: &effectusv1.Value_ResultSlot{ResultSlot: slot}}, nil
}

func lowerASTLiteral(literal *ast.Literal) (*effectusv1.Literal, error) {
	if literal == nil {
		return nil, fmt.Errorf("literal is nil")
	}
	kinds := 0
	if literal.String != nil {
		kinds++
	}
	if literal.Int != nil {
		kinds++
	}
	if literal.Float != nil {
		kinds++
	}
	if literal.Bool != nil {
		kinds++
	}
	if literal.List != nil {
		kinds++
	}
	if literal.Map != nil {
		kinds++
	}
	if kinds != 1 {
		return nil, fmt.Errorf("literal must contain exactly one value kind")
	}
	switch {
	case literal.String != nil:
		return &effectusv1.Literal{Kind: &effectusv1.Literal_StringValue{StringValue: *literal.String}}, nil
	case literal.Int != nil:
		return &effectusv1.Literal{Kind: &effectusv1.Literal_IntValue{IntValue: int64(*literal.Int)}}, nil
	case literal.Float != nil:
		if math.IsNaN(*literal.Float) || math.IsInf(*literal.Float, 0) {
			return nil, fmt.Errorf("floating literal must be finite")
		}
		return &effectusv1.Literal{Kind: &effectusv1.Literal_DoubleValue{DoubleValue: *literal.Float}}, nil
	case literal.Bool != nil:
		return &effectusv1.Literal{Kind: &effectusv1.Literal_BoolValue{BoolValue: *literal.Bool}}, nil
	case literal.List != nil:
		values := make([]*effectusv1.Literal, len(literal.List))
		for index := range literal.List {
			value, err := lowerASTLiteral(&literal.List[index])
			if err != nil {
				return nil, fmt.Errorf("list item %d: %w", index, err)
			}
			values[index] = value
		}
		return &effectusv1.Literal{Kind: &effectusv1.Literal_ListValue{ListValue: &effectusv1.LiteralList{Values: values}}}, nil
	default:
		fields := make([]*effectusv1.LiteralField, 0, len(literal.Map))
		seen := make(map[string]struct{}, len(literal.Map))
		for _, entry := range literal.Map {
			if entry == nil || strings.TrimSpace(entry.Key) == "" {
				return nil, fmt.Errorf("object contains an invalid field")
			}
			if _, duplicate := seen[entry.Key]; duplicate {
				return nil, fmt.Errorf("object repeats field %q", entry.Key)
			}
			seen[entry.Key] = struct{}{}
			value, err := lowerASTLiteral(&entry.Value)
			if err != nil {
				return nil, fmt.Errorf("object field %q: %w", entry.Key, err)
			}
			fields = append(fields, &effectusv1.LiteralField{Name: entry.Key, Value: value})
		}
		sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
		return &effectusv1.Literal{Kind: &effectusv1.Literal_ObjectValue{ObjectValue: &effectusv1.LiteralObject{Fields: fields}}}, nil
	}
}

func conjunction(expressions []*effectusv1.Expression) *effectusv1.Expression {
	if len(expressions) == 0 {
		return trueExpression()
	}
	result := expressions[0]
	for _, expression := range expressions[1:] {
		result = binaryExpression(effectusv1.BinaryOperator_BINARY_OPERATOR_AND, result, expression)
	}
	return result
}

func trueExpression() *effectusv1.Expression {
	return &effectusv1.Expression{Kind: &effectusv1.Expression_Literal{Literal: &effectusv1.Literal{Kind: &effectusv1.Literal_BoolValue{BoolValue: true}}}}
}

func binaryExpression(operator effectusv1.BinaryOperator, left, right *effectusv1.Expression) *effectusv1.Expression {
	return &effectusv1.Expression{Kind: &effectusv1.Expression_Binary{Binary: &effectusv1.BinaryExpression{Operator: operator, Left: left, Right: right}}}
}

func lowerPredicate(expression string) (*effectusv1.Expression, error) {
	tree, err := exprparser.Parse(expression)
	if err != nil {
		return nil, fmt.Errorf("parse expression: %w", err)
	}
	return lowerExprNode(tree.Node)
}

func lowerExprNode(node exprast.Node) (*effectusv1.Expression, error) {
	switch node := node.(type) {
	case *exprast.NilNode:
		return &effectusv1.Expression{Kind: &effectusv1.Expression_Literal{Literal: &effectusv1.Literal{Kind: &effectusv1.Literal_Null{Null: effectusv1.NullValue_NULL_VALUE_NULL}}}}, nil
	case *exprast.BoolNode:
		return &effectusv1.Expression{Kind: &effectusv1.Expression_Literal{Literal: &effectusv1.Literal{Kind: &effectusv1.Literal_BoolValue{BoolValue: node.Value}}}}, nil
	case *exprast.IntegerNode:
		return &effectusv1.Expression{Kind: &effectusv1.Expression_Literal{Literal: &effectusv1.Literal{Kind: &effectusv1.Literal_IntValue{IntValue: int64(node.Value)}}}}, nil
	case *exprast.FloatNode:
		return &effectusv1.Expression{Kind: &effectusv1.Expression_Literal{Literal: &effectusv1.Literal{Kind: &effectusv1.Literal_DoubleValue{DoubleValue: node.Value}}}}, nil
	case *exprast.StringNode:
		return &effectusv1.Expression{Kind: &effectusv1.Expression_Literal{Literal: &effectusv1.Literal{Kind: &effectusv1.Literal_StringValue{StringValue: node.Value}}}}, nil
	case *exprast.ArrayNode:
		values := make([]*effectusv1.Literal, len(node.Nodes))
		for index, item := range node.Nodes {
			expression, err := lowerExprNode(item)
			if err != nil {
				return nil, err
			}
			literal, ok := expression.Kind.(*effectusv1.Expression_Literal)
			if !ok {
				return nil, fmt.Errorf("predicate array item %d is not a literal", index)
			}
			values[index] = literal.Literal
		}
		return &effectusv1.Expression{Kind: &effectusv1.Expression_Literal{Literal: &effectusv1.Literal{Kind: &effectusv1.Literal_ListValue{ListValue: &effectusv1.LiteralList{Values: values}}}}}, nil
	case *exprast.IdentifierNode, *exprast.MemberNode:
		path, err := expressionFactPath(node)
		if err != nil {
			return nil, err
		}
		return &effectusv1.Expression{Kind: &effectusv1.Expression_FactPath{FactPath: path}}, nil
	case *exprast.UnaryNode:
		operand, err := lowerExprNode(node.Node)
		if err != nil {
			return nil, err
		}
		var operator effectusv1.UnaryOperator
		switch node.Operator {
		case "!", "not":
			operator = effectusv1.UnaryOperator_UNARY_OPERATOR_NOT
		case "-":
			operator = effectusv1.UnaryOperator_UNARY_OPERATOR_NEGATE
		default:
			return nil, fmt.Errorf("unsupported unary operator %q", node.Operator)
		}
		return &effectusv1.Expression{Kind: &effectusv1.Expression_Unary{Unary: &effectusv1.UnaryExpression{Operator: operator, Operand: operand}}}, nil
	case *exprast.BinaryNode:
		left, err := lowerExprNode(node.Left)
		if err != nil {
			return nil, err
		}
		right, err := lowerExprNode(node.Right)
		if err != nil {
			return nil, err
		}
		operator, negate, err := lowerBinaryOperator(node.Operator)
		if err != nil {
			return nil, err
		}
		result := binaryExpression(operator, left, right)
		if negate {
			result = &effectusv1.Expression{Kind: &effectusv1.Expression_Unary{Unary: &effectusv1.UnaryExpression{Operator: effectusv1.UnaryOperator_UNARY_OPERATOR_NOT, Operand: result}}}
		}
		return result, nil
	case *exprast.CallNode:
		callee, ok := node.Callee.(*exprast.IdentifierNode)
		if !ok {
			return nil, fmt.Errorf("only named function calls are supported")
		}
		return lowerFunctionCall(callee.Value, node.Arguments)
	case *exprast.BuiltinNode:
		return lowerFunctionCall(node.Name, node.Arguments)
	default:
		return nil, fmt.Errorf("unsupported predicate AST node %T", node)
	}
}

func lowerFunctionCall(name string, sourceArguments []exprast.Node) (*effectusv1.Expression, error) {
	arguments := make([]*effectusv1.Expression, len(sourceArguments))
	for index, argument := range sourceArguments {
		lowered, err := lowerExprNode(argument)
		if err != nil {
			return nil, fmt.Errorf("function %q argument %d: %w", name, index, err)
		}
		arguments[index] = lowered
	}
	return &effectusv1.Expression{Kind: &effectusv1.Expression_Call{Call: &effectusv1.FunctionCall{Function: name, Arguments: arguments}}}, nil
}

func expressionFactPath(node exprast.Node) (string, error) {
	switch node := node.(type) {
	case *exprast.IdentifierNode:
		return node.Value, nil
	case *exprast.MemberNode:
		if node.Optional || node.Method {
			return "", fmt.Errorf("optional and method member access is unsupported")
		}
		base, err := expressionFactPath(node.Node)
		if err != nil {
			return "", err
		}
		switch property := node.Property.(type) {
		case *exprast.StringNode:
			return base + "." + property.Value, nil
		case *exprast.IntegerNode:
			return fmt.Sprintf("%s[%d]", base, property.Value), nil
		default:
			return "", fmt.Errorf("dynamic fact member access is unsupported")
		}
	default:
		return "", fmt.Errorf("expression %T is not a fact path", node)
	}
}

func lowerBinaryOperator(operator string) (effectusv1.BinaryOperator, bool, error) {
	switch operator {
	case "==":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_EQUAL, false, nil
	case "!=":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_NOT_EQUAL, false, nil
	case ">":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_GREATER, false, nil
	case ">=":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_GREATER_EQUAL, false, nil
	case "<":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_LESS, false, nil
	case "<=":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_LESS_EQUAL, false, nil
	case "&&", "and":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_AND, false, nil
	case "||", "or":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_OR, false, nil
	case "in":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_IN, false, nil
	case "not in":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_IN, true, nil
	case "contains":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_CONTAINS, false, nil
	case "not contains":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_CONTAINS, true, nil
	case "+":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_ADD, false, nil
	case "-":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_SUBTRACT, false, nil
	case "*":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_MULTIPLY, false, nil
	case "/":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_DIVIDE, false, nil
	case "%":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_MODULO, false, nil
	case "matches":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_MATCHES, false, nil
	case "not matches":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_MATCHES, true, nil
	case "startsWith":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_STARTS_WITH, false, nil
	case "not startsWith":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_STARTS_WITH, true, nil
	case "endsWith":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_ENDS_WITH, false, nil
	case "not endsWith":
		return effectusv1.BinaryOperator_BINARY_OPERATOR_ENDS_WITH, true, nil
	default:
		return effectusv1.BinaryOperator_BINARY_OPERATOR_UNSPECIFIED, false, fmt.Errorf("unsupported binary operator %q", operator)
	}
}
