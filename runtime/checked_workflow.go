package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/workflow"
)

type checkedWorkflowInvocationExecutor struct {
	generation *Generation
	store      workflow.OutboxStore
}

func (executor checkedWorkflowInvocationExecutor) RetryUnknownOutcome(request invocation.Request) bool {
	if executor.generation == nil || executor.generation.Checked() == nil {
		return false
	}
	for _, plan := range executor.generation.Checked().CloneArtifact().Plans {
		for _, step := range plan.Steps {
			if step.Id == request.Metadata.Saga.EffectID && step.Verb == request.Verb && step.ContractHash == request.ContractHash {
				return step.IdempotencyPolicy == effectusv1.IdempotencyPolicy_IDEMPOTENCY_POLICY_SINK_GUARANTEED
			}
		}
	}
	return false
}
func (executor checkedWorkflowInvocationExecutor) Invoke(ctx context.Context, request invocation.Request) invocation.Outcome {
	saga, err := executor.store.GetSaga(ctx, request.Metadata.Saga.SagaID)
	if err != nil {
		return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: err}
	}
	if executor.generation == nil || saga.PlanDigest != executor.generation.Checked().Digest() {
		return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: schema.ErrIdentityConflict}
	}
	resolved, ok := executor.generation.Executor(request.Verb)
	if !ok || resolved == nil {
		return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: fmt.Errorf("verb %q is unavailable", request.Verb)}
	}
	return resolved.Invoke(ctx, request)
}

func (engine *Engine) executeCheckedWorkflow(ctx context.Context, generation *Generation, namespace, executionID string, facts map[string]interface{}, selectedPlanIDs map[string]struct{}, waitMode WaitMode) error {
	resolvedFacts := cloneWorkflowFacts(facts)
	artifact := generation.Checked().CloneArtifact()
	dispatcherOptions := engine.workflowOptions
	dispatcherOptions.RequestID = executionID

	selected := make([]*effectusv1.Plan, 0, len(artifact.Plans))
	for _, plan := range artifact.Plans {
		if err := ctx.Err(); err != nil {
			return err
		}
		if selectedPlanIDs != nil {
			if _, selectedAtAdmission := selectedPlanIDs[plan.Id]; !selectedAtAdmission {
				continue
			}
		} else {
			matches, err := evaluateCheckedPredicate(plan.GetPredicate().GetExpression(), resolvedFacts, generation)
			if err != nil {
				return fmt.Errorf("evaluate checked plan %q predicate: %w", plan.Id, err)
			}
			if !matches {
				continue
			}
		}
		if plan.ExecutionPolicy != effectusv1.ExecutionPolicy_EXECUTION_POLICY_DURABLE_FAIL_FAST && plan.ExecutionPolicy != effectusv1.ExecutionPolicy_EXECUTION_POLICY_DURABLE_COMPENSATING {
			return fmt.Errorf("checked extension plan %q has unsupported execution policy %s", plan.GetId(), plan.ExecutionPolicy)
		}
		selected = append(selected, plan)
		sagaID := schema.StableSagaID(executionID, plan.Id)
		if _, err := engine.workflowStore.CreateSaga(ctx, schema.CreateSagaRequest{
			Namespace: namespace, SagaID: sagaID, ExecutionID: executionID,
			PlanID: plan.Id, PlanDigest: generation.Checked().Digest(), Serial: true,
		}); err != nil {
			return fmt.Errorf("create checked workflow saga %q: %w", plan.Id, err)
		}
		if len(plan.Steps) == 0 {
			if err := engine.workflowStore.CompleteSaga(ctx, sagaID); err != nil {
				return fmt.Errorf("complete empty checked workflow saga %q: %w", plan.Id, err)
			}
			continue
		}
		if _, err := schema.EnqueueCheckedStep(ctx, engine.workflowStore, generation.Checked(), schema.CheckedEnqueueRequest{
			SagaID: sagaID, PlanID: plan.Id, EffectID: plan.Steps[0].Id, Facts: resolvedFacts,
		}); err != nil {
			return fmt.Errorf("enqueue plan %q first step: %w", plan.Id, err)
		}
	}
	if waitMode == WaitAccepted {
		return nil
	}
	for _, plan := range selected {
		sagaID := schema.StableSagaID(executionID, plan.Id)
		slots := make([]interface{}, 0, len(plan.Steps))
		for _, step := range plan.Steps {
			if err := ctx.Err(); err != nil {
				return err
			}
			stepOptions := dispatcherOptions
			if policy := step.GetRetryPolicy(); policy != nil {
				stepOptions.MaxAttempts = uint64(policy.MaxAttempts)
				stepOptions.InitialBackoff = time.Duration(policy.InitialBackoffMillis) * time.Millisecond
				stepOptions.MaxBackoff = time.Duration(policy.MaxBackoffMillis) * time.Millisecond
			}
			dispatcher, err := schema.NewDispatcher(engine.workflowStore, engine.workflowFencing, checkedWorkflowInvocationExecutor{generation: generation, store: engine.workflowStore}, stepOptions)
			if err != nil {
				return fmt.Errorf("create checked workflow dispatcher: %w", err)
			}
			dispatch, err := schema.EnqueueCheckedStep(ctx, engine.workflowStore, generation.Checked(), schema.CheckedEnqueueRequest{
				SagaID: sagaID, PlanID: plan.Id, EffectID: step.Id, Facts: resolvedFacts, ResultSlots: slots,
			})
			if err != nil {
				return fmt.Errorf("enqueue plan %q step %q: %w", plan.Id, step.Id, err)
			}
			completed, err := dispatchCheckedWorkflowStep(ctx, dispatcher, engine.workflowStore, dispatch.ID)
			if err != nil {
				if plan.ExecutionPolicy == effectusv1.ExecutionPolicy_EXECUTION_POLICY_DURABLE_COMPENSATING {
					if compensationErr := driveCheckedCompensation(ctx, dispatcher, engine.workflowStore, sagaID); compensationErr != nil {
						return errors.Join(fmt.Errorf("plan %q step %q: %w", plan.Id, step.Id, err), compensationErr)
					}
				}
				return fmt.Errorf("plan %q step %q: %w", plan.Id, step.Id, err)
			}
			if step.ResultSlot != nil {
				if *step.ResultSlot != uint32(len(slots)) {
					return fmt.Errorf("plan %q step %q has a non-dense result slot", plan.Id, step.Id)
				}
				result, err := decodeCheckedWorkflowResult(completed.Result)
				if err != nil {
					return fmt.Errorf("decode plan %q step %q result: %w", plan.Id, step.Id, err)
				}
				slots = append(slots, result)
			}
		}
		if err := engine.workflowStore.CompleteSaga(ctx, sagaID); err != nil {
			return fmt.Errorf("complete checked workflow saga %q: %w", plan.Id, err)
		}
	}
	return nil
}

func dispatchCheckedWorkflowStep(ctx context.Context, dispatcher *schema.Dispatcher, store workflow.OutboxStore, dispatchID string) (*workflow.Dispatch, error) {
	for {
		current, err := store.GetDispatch(ctx, dispatchID)
		if err != nil {
			return nil, err
		}
		switch current.State {
		case schema.DispatchSucceeded:
			return current, nil
		case schema.DispatchFailedPermanent, schema.DispatchBlockedUnknown, schema.DispatchBlockedFence:
			return nil, fmt.Errorf("durable dispatch entered terminal state %s: %s", current.State, current.LastError)
		}
		_, err = dispatcher.Dispatch(ctx, dispatchID)
		if err == nil {
			continue
		}
		if !errors.Is(err, schema.ErrNoDispatch) {
			return nil, err
		}
		delay := 10 * time.Millisecond
		if current.NextAttemptAt.After(time.Now()) {
			delay = time.Until(current.NextAttemptAt)
			if delay > time.Second {
				delay = time.Second
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func driveCheckedCompensation(ctx context.Context, dispatcher *schema.Dispatcher, store workflow.OutboxStore, sagaID string) error {
	for {
		saga, err := store.GetSaga(ctx, sagaID)
		if err != nil {
			return err
		}
		switch saga.State {
		case schema.SagaCompensated:
			return nil
		case schema.SagaBlockedUnknown, schema.SagaBlockedFence, schema.SagaBlockedDependency, schema.SagaBlockedCompensation, schema.SagaFailed:
			return fmt.Errorf("compensation entered state %s", saga.State)
		case schema.SagaCompensating:
		default:
			return fmt.Errorf("compensation expected compensating saga, got %s", saga.State)
		}
		dispatches, err := store.ListDispatches(ctx, sagaID)
		if err != nil {
			return err
		}
		var candidate *schema.Dispatch
		for _, item := range dispatches {
			if item.Direction == invocation.DirectionCompensation && (item.State == schema.DispatchQueued || item.State == schema.DispatchRetryWait || item.State == schema.DispatchInFlight) {
				candidate = item
				break
			}
		}
		if candidate == nil {
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		_, err = dispatcher.Dispatch(ctx, candidate.ID)
		if err != nil && !errors.Is(err, schema.ErrNoDispatch) {
			return err
		}
	}
}

func decodeCheckedWorkflowResult(raw json.RawMessage) (interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var result interface{}
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func evaluateCheckedPredicate(expression *effectusv1.Expression, facts map[string]interface{}, generation *Generation) (bool, error) {
	value, err := evaluateCheckedExpression(expression, facts, generation)
	if err != nil {
		return false, err
	}
	result, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("predicate returned %T, want bool", value)
	}
	return result, nil
}

func evaluateCheckedExpression(expression *effectusv1.Expression, facts map[string]interface{}, generation *Generation) (interface{}, error) {
	if expression == nil {
		return nil, fmt.Errorf("expression is nil")
	}
	switch kind := expression.Kind.(type) {
	case *effectusv1.Expression_Literal:
		return checkedLiteralValue(kind.Literal)
	case *effectusv1.Expression_FactPath:
		value, ok := lookupWorkflowFact(facts, kind.FactPath)
		if !ok {
			return nil, fmt.Errorf("fact %q is missing", kind.FactPath)
		}
		return value, nil
	case *effectusv1.Expression_Unary:
		if kind.Unary == nil {
			return nil, fmt.Errorf("unary expression is nil")
		}
		value, err := evaluateCheckedExpression(kind.Unary.Operand, facts, generation)
		if err != nil {
			return nil, err
		}
		switch kind.Unary.Operator {
		case effectusv1.UnaryOperator_UNARY_OPERATOR_NOT:
			boolean, ok := value.(bool)
			if !ok {
				return nil, fmt.Errorf("not operand is %T", value)
			}
			return !boolean, nil
		case effectusv1.UnaryOperator_UNARY_OPERATOR_NEGATE:
			if integer, ok := checkedInteger(value); ok {
				return -integer, nil
			}
			if floating, ok := checkedFloat(value); ok {
				return -floating, nil
			}
			return nil, fmt.Errorf("negate operand is %T", value)
		default:
			return nil, fmt.Errorf("unsupported unary operator %s", kind.Unary.Operator)
		}
	case *effectusv1.Expression_Binary:
		if kind.Binary == nil {
			return nil, fmt.Errorf("binary expression is nil")
		}
		left, err := evaluateCheckedExpression(kind.Binary.Left, facts, generation)
		if err != nil {
			return nil, err
		}
		// Preserve short-circuit behavior for checked logical operators.
		if kind.Binary.Operator == effectusv1.BinaryOperator_BINARY_OPERATOR_AND {
			if boolean, ok := left.(bool); ok && !boolean {
				return false, nil
			}
		}
		if kind.Binary.Operator == effectusv1.BinaryOperator_BINARY_OPERATOR_OR {
			if boolean, ok := left.(bool); ok && boolean {
				return true, nil
			}
		}
		right, err := evaluateCheckedExpression(kind.Binary.Right, facts, generation)
		if err != nil {
			return nil, err
		}
		return evaluateCheckedBinary(kind.Binary.Operator, left, right)
	case *effectusv1.Expression_Call:
		return nil, fmt.Errorf("function calls are not available in an immutable source generation")
	default:
		return nil, fmt.Errorf("expression kind is not set")
	}
}

func evaluateCheckedBinary(operator effectusv1.BinaryOperator, left, right interface{}) (interface{}, error) {
	switch operator {
	case effectusv1.BinaryOperator_BINARY_OPERATOR_EQUAL:
		return reflect.DeepEqual(normalizeCheckedNumber(left), normalizeCheckedNumber(right)), nil
	case effectusv1.BinaryOperator_BINARY_OPERATOR_NOT_EQUAL:
		return !reflect.DeepEqual(normalizeCheckedNumber(left), normalizeCheckedNumber(right)), nil
	case effectusv1.BinaryOperator_BINARY_OPERATOR_AND:
		leftBool, leftOK := left.(bool)
		rightBool, rightOK := right.(bool)
		if !leftOK || !rightOK {
			return nil, fmt.Errorf("logical operands must be bool")
		}
		return leftBool && rightBool, nil
	case effectusv1.BinaryOperator_BINARY_OPERATOR_OR:
		leftBool, leftOK := left.(bool)
		rightBool, rightOK := right.(bool)
		if !leftOK || !rightOK {
			return nil, fmt.Errorf("logical operands must be bool")
		}
		return leftBool || rightBool, nil
	case effectusv1.BinaryOperator_BINARY_OPERATOR_GREATER,
		effectusv1.BinaryOperator_BINARY_OPERATOR_GREATER_EQUAL,
		effectusv1.BinaryOperator_BINARY_OPERATOR_LESS,
		effectusv1.BinaryOperator_BINARY_OPERATOR_LESS_EQUAL:
		comparison, err := compareCheckedValues(left, right)
		if err != nil {
			return nil, err
		}
		switch operator {
		case effectusv1.BinaryOperator_BINARY_OPERATOR_GREATER:
			return comparison > 0, nil
		case effectusv1.BinaryOperator_BINARY_OPERATOR_GREATER_EQUAL:
			return comparison >= 0, nil
		case effectusv1.BinaryOperator_BINARY_OPERATOR_LESS:
			return comparison < 0, nil
		default:
			return comparison <= 0, nil
		}
	case effectusv1.BinaryOperator_BINARY_OPERATOR_CONTAINS:
		return checkedContains(left, right)
	case effectusv1.BinaryOperator_BINARY_OPERATOR_IN:
		return checkedContains(right, left)
	case effectusv1.BinaryOperator_BINARY_OPERATOR_STARTS_WITH,
		effectusv1.BinaryOperator_BINARY_OPERATOR_ENDS_WITH,
		effectusv1.BinaryOperator_BINARY_OPERATOR_MATCHES:
		leftString, leftOK := left.(string)
		rightString, rightOK := right.(string)
		if !leftOK || !rightOK {
			return nil, fmt.Errorf("string operands are required")
		}
		switch operator {
		case effectusv1.BinaryOperator_BINARY_OPERATOR_STARTS_WITH:
			return strings.HasPrefix(leftString, rightString), nil
		case effectusv1.BinaryOperator_BINARY_OPERATOR_ENDS_WITH:
			return strings.HasSuffix(leftString, rightString), nil
		default:
			return regexp.MatchString(rightString, leftString)
		}
	case effectusv1.BinaryOperator_BINARY_OPERATOR_ADD:
		if leftString, ok := left.(string); ok {
			rightString, ok := right.(string)
			if !ok {
				return nil, fmt.Errorf("string add requires string operands")
			}
			return leftString + rightString, nil
		}
		return checkedArithmetic(operator, left, right)
	case effectusv1.BinaryOperator_BINARY_OPERATOR_SUBTRACT,
		effectusv1.BinaryOperator_BINARY_OPERATOR_MULTIPLY,
		effectusv1.BinaryOperator_BINARY_OPERATOR_DIVIDE,
		effectusv1.BinaryOperator_BINARY_OPERATOR_MODULO:
		return checkedArithmetic(operator, left, right)
	default:
		return nil, fmt.Errorf("unsupported binary operator %s", operator)
	}
}

func checkedArithmetic(operator effectusv1.BinaryOperator, left, right interface{}) (interface{}, error) {
	leftInt, leftIsInt := checkedInteger(left)
	rightInt, rightIsInt := checkedInteger(right)
	if leftIsInt && rightIsInt && operator != effectusv1.BinaryOperator_BINARY_OPERATOR_DIVIDE {
		switch operator {
		case effectusv1.BinaryOperator_BINARY_OPERATOR_ADD:
			return leftInt + rightInt, nil
		case effectusv1.BinaryOperator_BINARY_OPERATOR_SUBTRACT:
			return leftInt - rightInt, nil
		case effectusv1.BinaryOperator_BINARY_OPERATOR_MULTIPLY:
			return leftInt * rightInt, nil
		case effectusv1.BinaryOperator_BINARY_OPERATOR_MODULO:
			if rightInt == 0 {
				return nil, fmt.Errorf("modulo by zero")
			}
			return leftInt % rightInt, nil
		}
	}
	leftFloat, leftOK := checkedFloat(left)
	rightFloat, rightOK := checkedFloat(right)
	if !leftOK || !rightOK {
		return nil, fmt.Errorf("arithmetic operands must be numeric")
	}
	switch operator {
	case effectusv1.BinaryOperator_BINARY_OPERATOR_ADD:
		return leftFloat + rightFloat, nil
	case effectusv1.BinaryOperator_BINARY_OPERATOR_SUBTRACT:
		return leftFloat - rightFloat, nil
	case effectusv1.BinaryOperator_BINARY_OPERATOR_MULTIPLY:
		return leftFloat * rightFloat, nil
	case effectusv1.BinaryOperator_BINARY_OPERATOR_DIVIDE:
		if rightFloat == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return leftFloat / rightFloat, nil
	default:
		return nil, fmt.Errorf("modulo requires integer operands")
	}
}

func checkedContains(container, item interface{}) (bool, error) {
	if text, ok := container.(string); ok {
		value, ok := item.(string)
		if !ok {
			return false, fmt.Errorf("string contains requires string")
		}
		return strings.Contains(text, value), nil
	}
	value := reflect.ValueOf(container)
	if value.IsValid() && (value.Kind() == reflect.Slice || value.Kind() == reflect.Array) {
		for index := 0; index < value.Len(); index++ {
			if reflect.DeepEqual(normalizeCheckedNumber(value.Index(index).Interface()), normalizeCheckedNumber(item)) {
				return true, nil
			}
		}
		return false, nil
	}
	return false, fmt.Errorf("contains operand is %T", container)
}

func compareCheckedValues(left, right interface{}) (int, error) {
	if leftString, ok := left.(string); ok {
		rightString, ok := right.(string)
		if !ok {
			return 0, fmt.Errorf("comparison operands differ")
		}
		return strings.Compare(leftString, rightString), nil
	}
	leftFloat, leftOK := checkedFloat(left)
	rightFloat, rightOK := checkedFloat(right)
	if !leftOK || !rightOK {
		return 0, fmt.Errorf("comparison operands must be numeric or string")
	}
	if leftFloat < rightFloat {
		return -1, nil
	}
	if leftFloat > rightFloat {
		return 1, nil
	}
	return 0, nil
}

func checkedInteger(value interface{}) (int64, bool) {
	switch value := value.(type) {
	case int:
		return int64(value), true
	case int8:
		return int64(value), true
	case int16:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case uint:
		if uint64(value) <= math.MaxInt64 {
			return int64(value), true
		}
	case uint8:
		return int64(value), true
	case uint16:
		return int64(value), true
	case uint32:
		return int64(value), true
	case uint64:
		if value <= math.MaxInt64 {
			return int64(value), true
		}
	case json.Number:
		integer, err := value.Int64()
		return integer, err == nil
	}
	return 0, false
}

func checkedFloat(value interface{}) (float64, bool) {
	if integer, ok := checkedInteger(value); ok {
		return float64(integer), true
	}
	switch value := value.(type) {
	case float32:
		return float64(value), true
	case float64:
		return value, !math.IsNaN(value) && !math.IsInf(value, 0)
	case json.Number:
		floating, err := value.Float64()
		return floating, err == nil
	}
	return 0, false
}

func normalizeCheckedNumber(value interface{}) interface{} {
	if integer, ok := checkedInteger(value); ok {
		return integer
	}
	if floating, ok := checkedFloat(value); ok {
		return floating
	}
	return value
}

func isUnconditionalExtensionPlan(plan *effectusv1.Plan) bool {
	if plan == nil || plan.Predicate == nil || plan.Predicate.Expression == nil {
		return false
	}
	literal, ok := plan.Predicate.Expression.Kind.(*effectusv1.Expression_Literal)
	if !ok || literal.Literal == nil {
		return false
	}
	value, ok := literal.Literal.Kind.(*effectusv1.Literal_BoolValue)
	return ok && value.BoolValue
}

func resolveCheckedWorkflowValue(value *effectusv1.Value, facts map[string]interface{}, slots []interface{}) (interface{}, error) {
	if value == nil {
		return nil, fmt.Errorf("value is nil")
	}
	switch kind := value.Kind.(type) {
	case *effectusv1.Value_Literal:
		return checkedLiteralValue(kind.Literal)
	case *effectusv1.Value_FactPath:
		resolved, ok := lookupWorkflowFact(facts, kind.FactPath)
		if !ok {
			return nil, fmt.Errorf("fact %q is missing", kind.FactPath)
		}
		return resolved, nil
	case *effectusv1.Value_ResultSlot:
		if int(kind.ResultSlot) >= len(slots) {
			return nil, fmt.Errorf("result slot %d is unavailable", kind.ResultSlot)
		}
		return slots[kind.ResultSlot], nil
	default:
		return nil, fmt.Errorf("value kind is not set")
	}
}

func cloneWorkflowFacts(facts map[string]interface{}) map[string]interface{} {
	clone := make(map[string]interface{}, len(facts))
	for path, value := range facts {
		clone[path] = value
	}
	return clone
}

// mergeWorkflowFactOverrides applies nested values before explicit dotted
// paths. An explicit dotted path therefore wins a collision deterministically.
func mergeWorkflowFactOverrides(target, overrides map[string]interface{}) {
	for _, path := range orderedWorkflowFactKeys(overrides) {
		value := overrides[path]
		target[path] = value
		flattenWorkflowFact(target, path, value)
	}
}

func flattenWorkflowFact(target map[string]interface{}, prefix string, value interface{}) {
	object, ok := value.(map[string]interface{})
	if !ok {
		return
	}
	for _, key := range orderedWorkflowFactKeys(object) {
		child := object[key]
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		target[path] = child
		flattenWorkflowFact(target, path, child)
	}
}

func orderedWorkflowFactKeys(object map[string]interface{}) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		leftDotted := strings.Contains(keys[left], ".")
		rightDotted := strings.Contains(keys[right], ".")
		if leftDotted != rightDotted {
			return !leftDotted
		}
		return keys[left] < keys[right]
	})
	return keys
}

func lookupWorkflowFact(facts map[string]interface{}, path string) (interface{}, bool) {
	if value, exists := facts[path]; exists {
		return value, true
	}
	current := interface{}(facts)
	nested := true
	for start := 0; start < len(path); {
		end := start
		for end < len(path) && path[end] != '.' {
			end++
		}
		object, ok := current.(map[string]interface{})
		if !ok {
			nested = false
			break
		}
		current, ok = object[path[start:end]]
		if !ok {
			nested = false
			break
		}
		start = end + 1
	}
	if nested {
		return current, true
	}
	return nil, false
}

func checkedLiteralValue(literal *effectusv1.Literal) (interface{}, error) {
	if literal == nil {
		return nil, fmt.Errorf("literal is nil")
	}
	switch kind := literal.Kind.(type) {
	case *effectusv1.Literal_Null:
		return nil, nil
	case *effectusv1.Literal_BoolValue:
		return kind.BoolValue, nil
	case *effectusv1.Literal_IntValue:
		return kind.IntValue, nil
	case *effectusv1.Literal_DoubleValue:
		return kind.DoubleValue, nil
	case *effectusv1.Literal_StringValue:
		return kind.StringValue, nil
	case *effectusv1.Literal_BytesValue:
		return append([]byte(nil), kind.BytesValue...), nil
	case *effectusv1.Literal_ListValue:
		if kind.ListValue == nil {
			return nil, fmt.Errorf("list literal is nil")
		}
		values := make([]interface{}, len(kind.ListValue.Values))
		for index, value := range kind.ListValue.Values {
			resolved, err := checkedLiteralValue(value)
			if err != nil {
				return nil, err
			}
			values[index] = resolved
		}
		return values, nil
	case *effectusv1.Literal_ObjectValue:
		if kind.ObjectValue == nil {
			return nil, fmt.Errorf("object literal is nil")
		}
		object := make(map[string]interface{}, len(kind.ObjectValue.Fields))
		for _, field := range kind.ObjectValue.Fields {
			resolved, err := checkedLiteralValue(field.Value)
			if err != nil {
				return nil, err
			}
			object[field.Name] = resolved
		}
		return object, nil
	default:
		return nil, fmt.Errorf("literal kind is not set")
	}
}
