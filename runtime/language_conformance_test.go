package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/compiler"
	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

func languageEnvironment() ir.Environment {
	return ir.Environment{Facts: map[string]string{
		"n": "int", "f": "float", "s": "string", "ready": "bool", "nothing": "null", "maximum": "int", "minimum": "int",
		"tags": "list<string>", "named": "Tags", "counts": "map<int>", "scores": "Scores", "profile": "Profile", "blob": "bytes", "blobs": "list<bytes>", "large": "int", "rounded": "float",
	}, Types: map[string]ir.TypeDefinition{
		"Tags":    {Kind: ir.TypeKindList, ElementType: "string"},
		"Scores":  {Kind: ir.TypeKindMap, ElementType: "int"},
		"Profile": {Kind: ir.TypeKindObject, Fields: map[string]string{"tags": "Tags", "scores": "Scores", "blob": "bytes"}, RequiredFields: []string{"tags", "scores", "blob"}},
	}}
}
func languageFacts() map[string]any {
	return map[string]any{"n": json.Number("6.0"), "f": 2.5, "s": "vip-order", "ready": true, "nothing": nil, "maximum": int64(math.MaxInt64), "minimum": int64(math.MinInt64),
		"tags": []string{"vip", "paid"}, "named": []any{"vip", "paid"}, "counts": map[string]int{"a": 1}, "scores": map[string]any{"a": json.Number("1.0")},
		"profile": map[string]any{"tags": []string{"vip"}, "scores": map[string]int{"a": 1}, "blob": []byte{0, 255}},
		"blob":    []byte{0, 255}, "blobs": [][]byte{{0, 255}}, "large": int64(9007199254740993), "rounded": float64(9007199254740992)}
}
func compileLanguage(t *testing.T, env ir.Environment, source string, dialect string) *ir.Checked {
	t.Helper()
	b, err := bundle.New(bundle.Spec{Name: "language", Version: "1", Environment: env, Sources: []bundle.Source{{Path: "language." + dialect, Content: source}}})
	require.NoError(t, err)
	checked, err := compiler.CompileChecked(t.Context(), b, compiler.CompileOptions{})
	require.NoError(t, err)
	again, err := ir.Check(checked.CloneArtifact(), env, ir.Limits{})
	require.NoError(t, err)
	require.Equal(t, checked.Digest(), again.Digest())
	parsed, err := ir.Parse(checked.Marshal(), env, ir.Limits{})
	require.NoError(t, err)
	require.Equal(t, checked.Digest(), parsed.Digest())
	return parsed
}
func languageGeneration(t *testing.T, env ir.Environment, checked *ir.Checked, executors map[string]invocation.Executor) *Generation {
	t.Helper()
	descriptors := map[string]invocation.Descriptor{}
	for verb := range executors {
		descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorEmbedded})
		require.NoError(t, err)
		descriptors[verb] = descriptor
	}
	generation, err := NewGeneration(GenerationConfig{Checked: checked, Environment: env, Ruleset: "language", Version: "1", SourceDigest: strings.Repeat("a", 64), Executors: executors, ExecutorDescriptors: descriptors})
	require.NoError(t, err)
	return generation
}
func languageEngine(t *testing.T, generation *Generation, store schema.OutboxStore, ledger schema.ExecutionLedger) *Engine {
	t.Helper()
	engine, err := NewEngine(generation)
	require.NoError(t, err)
	require.NoError(t, engine.ConfigureWorkflow(store, nil, schema.DispatcherOptions{Owner: "language"}))
	require.NoError(t, engine.ConfigureLedger(ledger, nil))
	t.Cleanup(func() { require.NoError(t, engine.Close()) })
	return engine
}

func TestLanguageConformanceCompileCheckDryRunAdmitExecuteReparse(t *testing.T) {
	expressions := []string{
		`ready && !false`, `false || ready`, `n == 6`, `n != 5`, `n > 5 && n >= 6 && n < 7 && n <= 6`,
		`(n + 2) == 8 && (n - 2) == 4 && (n * 2) == 12 && (n / 2) == 3 && (n % 4) == 2`,
		`-n == -6 && -f == -2.5`, `(f + 0.5) == 3 && (f * 2) == 5`,
		`s contains "order" && s startsWith "vip" && s endsWith "order" && s matches "^vip-"`,
		`(s + "!") == "vip-order!" && s > "abc"`,
		`"vip" in tags && named contains "paid"`, `tags == named`, `counts == scores`,
		`n in [4, 6, 8] && n not in [1, 2]`, `nothing == nil`, `blob in blobs`,
		`large > rounded && large != rounded`,
		`maximum == 9223372036854775807 && minimum == (-9223372036854775807 - 1) && minimum < maximum && minimum < 0`,
		// Short-circuit must not divide by zero.
		`true || n / 0 > 1`, `!(false && n / 0 > 1)`,
	}
	env := languageEnvironment()
	facts := languageFacts()
	for i, expression := range expressions {
		t.Run(expression, func(t *testing.T) {
			checked := compileLanguage(t, env, fmt.Sprintf(`rule "conform" priority 1 { when { %s } then {} }`, expression), "eff")
			generation := languageGeneration(t, env, checked, nil)
			store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
			engine := languageEngine(t, generation, store, ledger)
			dry, err := engine.DryRun(t.Context(), facts)
			require.NoError(t, err)
			require.True(t, dry[0].Matched)
			id := fmt.Sprintf("case-%d", i)
			accepted, err := engine.Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: id, AdmissionID: id, TenantNamespace: "test", Ruleset: "language", Version: "1", Facts: facts}, WaitMode: WaitAccepted})
			require.NoError(t, err)
			require.True(t, accepted.DurablyAccepted)
			// Frozen JSON facts must evaluate identically, not just replay selected IDs.
			record, err := ledger.GetExecution(t.Context(), id)
			require.NoError(t, err)
			decoded, err := decodeExecutionFacts(record.EffectiveFacts)
			require.NoError(t, err)
			after, err := engine.DryRun(t.Context(), decoded)
			require.NoError(t, err)
			require.True(t, after[0].Matched)
			restarted := languageEngine(t, languageGeneration(t, env, checked, nil), store, ledger)
			done, err := restarted.Execute(t.Context(), ExecuteRequest{ResumeExecutionID: id, WaitMode: WaitTerminal})
			require.NoError(t, err)
			require.True(t, done.Completed)
		})
	}
	require.IsType(t, []string{}, facts["tags"])
	require.Equal(t, json.Number("6.0"), facts["n"])
}

type languageExecutor func(context.Context, invocation.Request) invocation.Outcome

func (f languageExecutor) Invoke(ctx context.Context, r invocation.Request) invocation.Outcome {
	return f(ctx, r)
}

func TestLanguageConformanceFlowCollectionsBytesAndResultSlots(t *testing.T) {
	env := languageEnvironment()
	env.Verbs = map[string]ir.VerbContract{
		"Capture": {Arguments: map[string]string{"profile": "Profile", "blob": "bytes", "counts": "map<int>"}, ResultType: "Profile"},
		"Consume": {Arguments: map[string]string{"profile": "Profile"}, ResultType: "void"},
	}
	checked := compileLanguage(t, env, `flow "flow" priority 1 { when { tags contains "vip" } steps { result = Capture(profile: profile, blob: blob, counts: counts) Consume(profile: $result) } }`, "effx")
	calls := 0
	executor := languageExecutor(func(_ context.Context, r invocation.Request) invocation.Outcome {
		calls++
		require.Equal(t, "AP8=", r.Arguments["profile"].(map[string]any)["blob"])
		if r.Verb == "Capture" {
			require.Equal(t, "AP8=", r.Arguments["blob"])
			return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: r.Arguments["profile"]}
		}
		return invocation.Outcome{Class: invocation.OutcomeSuccess}
	})
	executors := map[string]invocation.Executor{"Capture": executor, "Consume": executor}
	store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
	engine := languageEngine(t, languageGeneration(t, env, checked, executors), store, ledger)
	_, err := engine.DryRun(t.Context(), languageFacts())
	require.NoError(t, err)
	accepted, err := engine.Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: "flow", TenantNamespace: "test", Ruleset: "language", Version: "1", Facts: languageFacts()}, WaitMode: WaitAccepted})
	require.NoError(t, err)
	require.Zero(t, calls)
	restarted := languageEngine(t, languageGeneration(t, env, checked, executors), store, ledger)
	done, err := restarted.Execute(t.Context(), ExecuteRequest{ResumeExecutionID: accepted.ExecutionID, WaitMode: WaitTerminal})
	require.NoError(t, err)
	require.True(t, done.Completed)
	require.Equal(t, 2, calls)
}

func TestLanguageConformanceEmptyCollections(t *testing.T) {
	env := languageEnvironment()
	checked := compileLanguage(t, env, `rule "empty" priority 1 { when { tags == named && counts == scores } then {} }`, "eff")
	engine := languageEngine(t, languageGeneration(t, env, checked, nil), schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger())
	facts := languageFacts()
	facts["tags"] = []string(nil)
	facts["named"] = []any{}
	facts["counts"] = map[string]int(nil)
	facts["scores"] = map[string]any{}
	dry, err := engine.DryRun(t.Context(), facts)
	require.NoError(t, err)
	require.True(t, dry[0].Matched)
	done, err := engine.Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: "empty", TenantNamespace: "test", Ruleset: "language", Version: "1", Facts: facts}, WaitMode: WaitTerminal})
	require.NoError(t, err)
	require.True(t, done.Completed)
}

type languagePanickingMap map[string]int

func (languagePanickingMap) MarshalJSON() ([]byte, error) { panic("caller marshaler must not execute") }

func TestLanguageConformanceDoesNotInvokeFactMarshalers(t *testing.T) {
	env := languageEnvironment()
	checked := compileLanguage(t, env, `rule "safe" priority 1 { when { counts == scores } then {} }`, "eff")
	engine := languageEngine(t, languageGeneration(t, env, checked, nil), schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger())
	facts := languageFacts()
	facts["counts"] = languagePanickingMap{"a": 1}
	done, err := engine.Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: "no-callback", TenantNamespace: "test", Ruleset: "language", Version: "1", Facts: facts}, WaitMode: WaitTerminal})
	require.NoError(t, err)
	require.True(t, done.Completed)
}

func TestLanguageConformanceInvalidResultsDoNotReachNextStep(t *testing.T) {
	env := ir.Environment{Verbs: map[string]ir.VerbContract{
		"Produce": {ResultType: "list<int>"}, "Consume": {Arguments: map[string]string{"values": "list<int>"}, ResultType: "void"},
	}}
	checked := compileLanguage(t, env, `flow "result" priority 1 { when {} steps { values = Produce() Consume(values: $values) } }`, "effx")
	calls := 0
	executor := languageExecutor(func(_ context.Context, r invocation.Request) invocation.Outcome {
		calls++
		require.Equal(t, "Produce", r.Verb)
		return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: []any{1, "bad"}}
	})
	outbox, durable := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
	engine := languageEngine(t, languageGeneration(t, env, checked, map[string]invocation.Executor{"Produce": executor, "Consume": executor}), outbox, durable)
	request := ExecuteRequest{Admission: &Admission{ExecutionID: "bad-result", TenantNamespace: "test", Ruleset: "language", Version: "1"}, WaitMode: WaitTerminal}
	result, err := engine.Execute(t.Context(), request)
	require.ErrorContains(t, err, "list item 1")
	require.Equal(t, 1, calls)
	var terminal *TerminalExecutionError
	require.ErrorAs(t, err, &terminal)
	require.ErrorIs(t, err, ErrBlockedDependency)
	require.Equal(t, schema.ExecutionBlockedDependency, terminal.State)
	require.Equal(t, string(terminal.State), result.State)
	record, err := durable.GetExecution(t.Context(), "bad-result")
	require.NoError(t, err)
	require.Equal(t, schema.ExecutionBlockedDependency, record.State)
	require.Len(t, record.Plans, 1)
	saga, err := outbox.GetSaga(t.Context(), record.Plans[0].SagaID)
	require.NoError(t, err)
	// This is an execution-level dependency block, not a fabricated business
	// failure or completed saga. Preserve the unfinished saga and its success.
	require.Equal(t, schema.SagaRunning, saga.State)
	dispatches, err := outbox.ListDispatches(t.Context(), saga.SagaID)
	require.NoError(t, err)
	require.Len(t, dispatches, 1)
	require.Equal(t, schema.DispatchSucceeded, dispatches[0].State)
	require.Equal(t, invocation.OutcomeSuccess, dispatches[0].LastOutcome)
	require.Equal(t, `[1,"bad"]`, string(dispatches[0].Result))
	attempts, err := outbox.ListAttempts(t.Context(), dispatches[0].ID)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.Equal(t, invocation.OutcomeSuccess, attempts[0].Outcome)
	worker := &RecoveryWorker{Engine: engine, Store: durable, Owner: "bad-result-recovery", BatchSize: 2}
	processed, err := worker.RunOnce(t.Context())
	require.NoError(t, err)
	require.Zero(t, processed, "immutable invalid results must not remain recovery-eligible")
	for _, replay := range []ExecuteRequest{request, {ResumeExecutionID: "bad-result", WaitMode: WaitTerminal}} {
		_, err := engine.Execute(t.Context(), replay)
		require.ErrorAs(t, err, &terminal)
		require.Equal(t, schema.ExecutionBlockedDependency, terminal.State)
	}
	request.WaitMode = WaitAccepted
	accepted, err := engine.Execute(t.Context(), request)
	require.NoError(t, err)
	require.True(t, accepted.DurablyAccepted)
	require.False(t, accepted.Completed)
	require.Equal(t, string(schema.ExecutionBlockedDependency), accepted.State)
	require.Equal(t, 1, calls)
	unchanged, err := outbox.GetDispatch(t.Context(), dispatches[0].ID)
	require.NoError(t, err)
	require.Equal(t, dispatches[0], unchanged)
}

func TestLanguageConformanceInvalidFactsFailBeforeAdmission(t *testing.T) {
	env := languageEnvironment()
	checked := compileLanguage(t, env, `rule "invalid" priority 1 { when { true } then {} }`, "eff")
	store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
	engine := languageEngine(t, languageGeneration(t, env, checked, nil), store, ledger)
	for i, test := range []struct {
		path  string
		value any
		part  string
	}{
		{"tags", []any{"ok", false}, "list item 1"}, {"named", []any{false}, "list item 0"},
		{"counts", map[string]any{"a": "bad"}, `field "a"`}, {"scores", map[string]any{"a": false}, `field "a"`},
		{"profile", map[string]any{"tags": []any{false}}, `field "tags": list item 0`},
		{"blob", "bad", "base64"}, {"n", uint64(math.MaxInt64) + 1, "want int"}, {"f", math.Inf(1), "want float"},
	} {
		t.Run(fmt.Sprintf("%s-%d", test.path, i), func(t *testing.T) {
			facts := languageFacts()
			facts[test.path] = test.value
			_, err := engine.DryRun(t.Context(), facts)
			require.ErrorContains(t, err, test.part)
			id := fmt.Sprintf("invalid-%d", i)
			_, err = engine.Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: id, TenantNamespace: "test", Ruleset: "language", Version: "1", Facts: facts}, WaitMode: WaitTerminal})
			require.Error(t, err)
			_, err = ledger.GetExecution(t.Context(), id)
			require.ErrorIs(t, err, schema.ErrExecutionNotFound)
		})
	}
}

func TestGenerationRejectsGenericIRFunctionsIncludingUnreachableBranches(t *testing.T) {
	env := ir.Environment{Functions: map[string]ir.FunctionContract{"availableOnPaper": {ReturnType: "bool", Pure: true, Total: true}}}
	checked := compileLanguage(t, env, `rule "functions" priority 1 { when { true } then {} }`, "eff")
	artifact := checked.CloneArtifact()
	call := &effectusv1.Expression{Kind: &effectusv1.Expression_Call{Call: &effectusv1.FunctionCall{Function: "availableOnPaper"}}}
	artifact.Plans[0].Predicate.Expression = &effectusv1.Expression{Kind: &effectusv1.Expression_Binary{Binary: &effectusv1.BinaryExpression{Operator: effectusv1.BinaryOperator_BINARY_OPERATOR_OR, Left: artifact.Plans[0].Predicate.Expression, Right: call}}}
	checked, err := ir.Check(artifact, env, ir.Limits{})
	require.NoError(t, err)
	checked, err = ir.Parse(checked.Marshal(), env, ir.Limits{})
	require.NoError(t, err)
	for _, production := range []bool{false, true} {
		_, err := NewGeneration(GenerationConfig{Checked: checked, Environment: env, Ruleset: "language", Version: "1", SourceDigest: "source", Production: production, FunctionIDs: map[string]string{"availableOnPaper": "claimed/identity"}})
		require.ErrorContains(t, err, `function "availableOnPaper" is unavailable`)
	}
}

func TestCheckedWorkflowBytesLiteralAndExactNumericEquality(t *testing.T) {
	literal := &effectusv1.Literal{Kind: &effectusv1.Literal_BytesValue{BytesValue: []byte{0, 255}}}
	value, err := checkedLiteralValue(literal)
	require.NoError(t, err)
	normalized, err := ir.NormalizeValue(ir.Environment{}, "bytes", []byte{0, 255})
	require.NoError(t, err)
	require.True(t, checkedEqual(value, normalized))
	require.True(t, checkedEqual(int64(1), float64(1)))
	require.False(t, checkedEqual(int64(9007199254740993), float64(9007199254740992)))
	require.True(t, checkedEqual([]any{json.Number("1")}, []any{float64(1)}))
	_, err = checkedArithmetic(effectusv1.BinaryOperator_BINARY_OPERATOR_ADD, int64(math.MaxInt64), int64(1))
	require.ErrorContains(t, err, "overflow")
	_, err = checkedArithmetic(effectusv1.BinaryOperator_BINARY_OPERATOR_MULTIPLY, math.MaxFloat64, 2.0)
	require.ErrorContains(t, err, "overflow")
}

func TestLanguageConformanceGenericIRBytesLiteral(t *testing.T) {
	env := ir.Environment{Facts: map[string]string{"blob": "bytes"}, Verbs: map[string]ir.VerbContract{"Send": {Arguments: map[string]string{"blob": "bytes"}, ResultType: "void"}}}
	checked := compileLanguage(t, env, `rule "bytes" priority 1 { when { blob == blob } then { Send(blob: blob) } }`, "eff")
	artifact := checked.CloneArtifact()
	literal := &effectusv1.Literal{Kind: &effectusv1.Literal_BytesValue{BytesValue: []byte{0, 255}}}
	artifact.Plans[0].Predicate.Expression.GetBinary().Right = &effectusv1.Expression{Kind: &effectusv1.Expression_Literal{Literal: literal}}
	artifact.Plans[0].Steps[0].Arguments[0].Value = &effectusv1.Value{Kind: &effectusv1.Value_Literal{Literal: literal}}
	checked, err := ir.Check(artifact, env, ir.Limits{})
	require.NoError(t, err)
	checked, err = ir.Parse(checked.Marshal(), env, ir.Limits{})
	require.NoError(t, err)
	calls := 0
	executor := languageExecutor(func(_ context.Context, r invocation.Request) invocation.Outcome {
		calls++
		require.Equal(t, "AP8=", r.Arguments["blob"])
		return invocation.Outcome{Class: invocation.OutcomeSuccess}
	})
	store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
	engine := languageEngine(t, languageGeneration(t, env, checked, map[string]invocation.Executor{"Send": executor}), store, ledger)
	for i, blob := range []any{[]byte{0, 255}, "AP8="} {
		facts := map[string]any{"blob": blob}
		dry, err := engine.DryRun(t.Context(), facts)
		require.NoError(t, err)
		require.True(t, dry[0].Matched)
		done, err := engine.Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: fmt.Sprintf("bytes-%d", i), TenantNamespace: "test", Ruleset: "language", Version: "1", Facts: facts}, WaitMode: WaitTerminal})
		require.NoError(t, err)
		require.True(t, done.Completed)
		record, err := ledger.GetExecution(t.Context(), done.ExecutionID)
		require.NoError(t, err)
		decoded, err := decodeExecutionFacts(record.EffectiveFacts)
		require.NoError(t, err)
		dry, err = engine.DryRun(t.Context(), decoded)
		require.NoError(t, err)
		require.True(t, dry[0].Matched)
	}
	require.Equal(t, 2, calls)
}

func TestLanguageConformanceGenericIRMinimumIntegerLiteral(t *testing.T) {
	env := ir.Environment{Facts: map[string]string{"minimum": "int"}}
	checked := compileLanguage(t, env, `rule "minimum" priority 1 { when { minimum == minimum } then {} }`, "eff")
	artifact := checked.CloneArtifact()
	artifact.Plans[0].Predicate.Expression.GetBinary().Right = &effectusv1.Expression{Kind: &effectusv1.Expression_Literal{Literal: &effectusv1.Literal{Kind: &effectusv1.Literal_IntValue{IntValue: math.MinInt64}}}}
	checked, err := ir.Check(artifact, env, ir.Limits{})
	require.NoError(t, err)
	checked, err = ir.Parse(checked.Marshal(), env, ir.Limits{})
	require.NoError(t, err)
	require.Equal(t, int64(math.MinInt64), checked.CloneArtifact().Plans[0].Predicate.Expression.GetBinary().Right.GetLiteral().GetIntValue())
	store, ledger := schema.NewInMemoryOutboxStore(), schema.NewInMemoryExecutionLedger()
	engine := languageEngine(t, languageGeneration(t, env, checked, nil), store, ledger)
	facts := map[string]any{"minimum": json.Number("-9223372036854775808")}
	dry, err := engine.DryRun(t.Context(), facts)
	require.NoError(t, err)
	require.True(t, dry[0].Matched)
	done, err := engine.Execute(t.Context(), ExecuteRequest{Admission: &Admission{ExecutionID: "minimum", TenantNamespace: "test", Ruleset: "language", Version: "1", Facts: facts}, WaitMode: WaitTerminal})
	require.NoError(t, err)
	require.True(t, done.Completed)
	record, err := ledger.GetExecution(t.Context(), done.ExecutionID)
	require.NoError(t, err)
	decoded, err := decodeExecutionFacts(record.EffectiveFacts)
	require.NoError(t, err)
	require.Equal(t, json.Number("-9223372036854775808"), decoded["minimum"])
	dry, err = engine.DryRun(t.Context(), decoded)
	require.NoError(t, err)
	require.True(t, dry[0].Matched)
}

func TestAdmissionHashPreservesBaselineJSONIdentities(t *testing.T) {
	for _, test := range []struct {
		value    any
		expected string
	}{
		{map[string]any(nil), `{}`}, {map[string]any{}, `{}`}, {[]any(nil), `[]`}, {[]any{}, `[]`}, {nil, `null`},
		{map[string]any{"map": map[string]any(nil), "list": []any(nil), "null": nil}, `{"list":[],"map":{},"null":null}`},
		{map[string]any{"b": json.Number("1.0"), "a": json.Number("1e0")}, `{"a":1e0,"b":1.0}`},
		{map[string]any{"s": "text", "n": int64(9223372036854775807)}, `{"n":9223372036854775807,"s":"text"}`},
		{[]byte(nil), `""`}, {[]string(nil), `[]`}, {map[string]int(nil), `{}`},
	} {
		raw, err := canonicalJSONValue(test.value)
		require.NoError(t, err)
		require.Equal(t, test.expected, string(raw))
	}
	for _, golden := range []struct {
		facts map[string]any
		hash  string
	}{
		{nil, "96ba5ff9a61e60b9b4a23dba11a083f2717e7bf157a5b1569b998930d836946e"},
		{map[string]any{}, "96ba5ff9a61e60b9b4a23dba11a083f2717e7bf157a5b1569b998930d836946e"},
		{map[string]any{"list": []any(nil), "map": map[string]any(nil), "null": nil}, "796861d2b608fa80a985dcd61a0aba47637a39c028fa3cf612282090ffd02526"},
		{map[string]any{"a": json.Number("1e0"), "b": json.Number("1.0")}, "1d7e2228f7190a684ac12a4a1b138edbe421a85ac1a571875a63e828ed0fa724"},
		{map[string]any{"n": int64(math.MaxInt64), "s": "text"}, "e31aad99c214d062cea06d5351cf53b02bed9ada0f810deaf581242e83a348a2"},
	} {
		hash, err := admissionHash(&Admission{TenantNamespace: "test", Facts: golden.facts})
		require.NoError(t, err)
		require.Equal(t, golden.hash, hash)
	}
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	_, err := canonicalJSONValue(cyclic)
	require.ErrorContains(t, err, "depth")
	_, err = canonicalJSONValue(make([]any, 10001))
	require.ErrorContains(t, err, "limit")
	// These numeric lexical identities remain distinct until R11's explicit migration decision.
	a := &Admission{TenantNamespace: "test", Facts: map[string]any{"n": json.Number("1")}}
	first, err := admissionHash(a)
	require.NoError(t, err)
	a.Facts["n"] = json.Number("1.0")
	second, err := admissionHash(a)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.False(t, strings.Contains(first, " "))
}
