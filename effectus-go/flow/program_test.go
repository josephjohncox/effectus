package flow

import (
	"errors"
	"reflect"
	"testing"

	"github.com/effectus/effectus-go"
)

// Mock executor for testing
type MockExecutor struct {
	effects []effectus.Effect
	results []interface{}
	errors  []error
	index   int
}

func NewMockExecutor() *MockExecutor {
	return &MockExecutor{
		effects: make([]effectus.Effect, 0),
		results: make([]interface{}, 0),
		errors:  make([]error, 0),
	}
}

func (m *MockExecutor) Do(effect effectus.Effect) (interface{}, error) {
	m.effects = append(m.effects, effect)

	if m.index < len(m.results) {
		result := m.results[m.index]
		err := m.errors[m.index]
		m.index++
		return result, err
	}

	// Default behavior if no explicit results/errors defined
	return effect.Verb, nil
}

// Set up expected results and errors for sequential calls
func (m *MockExecutor) SetResults(results []interface{}, errors []error) {
	m.results = results
	m.errors = errors
	m.index = 0
}

// Return all recorded effects
func (m *MockExecutor) GetEffects() []effectus.Effect {
	return m.effects
}

func TestPure(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
	}{
		{name: "string value", value: "test"},
		{name: "int value", value: 42},
		{name: "boolean value", value: true},
		{name: "nil value", value: nil},
		{name: "map value", value: map[string]string{"key": "value"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program := Pure(tt.value)

			if program.Tag != PureProgramTag {
				t.Errorf("Expected PureProgramTag but got %v", program.Tag)
			}

			if !reflect.DeepEqual(program.Pure, tt.value) {
				t.Errorf("Expected value %v but got %v", tt.value, program.Pure)
			}
		})
	}
}

func TestDo(t *testing.T) {
	effect := effectus.Effect{
		Verb:    "TestVerb",
		Payload: map[string]interface{}{"key": "value"},
	}

	var resultReceived interface{}
	continuation := func(result interface{}) *Program {
		resultReceived = result
		return Pure("continued")
	}

	program := Do(effect, continuation)

	if program.Tag != EffectProgramTag {
		t.Errorf("Expected EffectProgramTag but got %v", program.Tag)
	}

	if program.Effect.Verb != "TestVerb" {
		t.Errorf("Expected verb 'TestVerb' but got %v", program.Effect.Verb)
	}

	// Test that continuation gets called correctly
	executor := NewMockExecutor()
	result, err := Run(program, executor)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if resultReceived != "TestVerb" {
		t.Errorf("Continuation received wrong result: %v", resultReceived)
	}

	if result != "continued" {
		t.Errorf("Expected final result 'continued' but got %v", result)
	}
}

func TestBind(t *testing.T) {
	tests := []struct {
		name     string
		program  *Program
		bindFn   func(interface{}) *Program
		expected interface{}
	}{
		{
			name:     "Bind on Pure",
			program:  Pure("initial"),
			bindFn:   func(v interface{}) *Program { return Pure(v.(string) + "-bound") },
			expected: "initial-bound",
		},
		{
			name: "Bind on Effect",
			program: Do(
				effectus.Effect{Verb: "TestVerb"},
				func(r interface{}) *Program { return Pure(r.(string) + "-continued") },
			),
			bindFn:   func(v interface{}) *Program { return Pure(v.(string) + "-bound") },
			expected: "TestVerb-continued-bound",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bound := tt.program.Bind(tt.bindFn)

			executor := NewMockExecutor()
			result, err := Run(bound, executor)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("Expected result %v but got %v", tt.expected, result)
			}
		})
	}
}

func TestMap(t *testing.T) {
	program := Pure(5)
	mapped := program.Map(func(v interface{}) interface{} {
		return v.(int) * 2
	})

	executor := NewMockExecutor()
	result, err := Run(mapped, executor)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if result != 10 {
		t.Errorf("Expected result 10 but got %v", result)
	}
}

func TestThen(t *testing.T) {
	first := Pure("first")
	second := Pure("second")

	sequenced := first.Then(second)

	executor := NewMockExecutor()
	result, err := Run(sequenced, executor)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if result != "second" {
		t.Errorf("Expected result 'second' but got %v", result)
	}
}

func TestFromList(t *testing.T) {
	effects := []effectus.Effect{
		{Verb: "First", Payload: map[string]interface{}{"id": 1}},
		{Verb: "Second", Payload: map[string]interface{}{"id": 2}},
		{Verb: "Third", Payload: map[string]interface{}{"id": 3}},
	}

	program := FromList(effects)

	executor := NewMockExecutor()
	_, err := Run(program, executor)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	executedEffects := executor.GetEffects()
	if len(executedEffects) != 3 {
		t.Errorf("Expected 3 effects but got %d", len(executedEffects))
	}

	for i, effect := range effects {
		if i < len(executedEffects) && executedEffects[i].Verb != effect.Verb {
			t.Errorf("Effect %d: expected verb '%s' but got '%s'", i, effect.Verb, executedEffects[i].Verb)
		}
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name          string
		program       *Program
		setResults    []interface{}
		setErrors     []error
		expectedValue interface{}
		expectError   bool
	}{
		{
			name:          "Run Pure program",
			program:       Pure("result"),
			expectedValue: "result",
			expectError:   false,
		},
		{
			name:          "Pure with error",
			program:       Pure(errors.New("test error")),
			expectedValue: nil,
			expectError:   true,
		},
		{
			name: "Run Effect program",
			program: Do(
				effectus.Effect{Verb: "TestVerb", Payload: map[string]interface{}{"id": 1}},
				func(r interface{}) *Program { return Pure(r) },
			),
			setResults:    []interface{}{"custom result"},
			setErrors:     []error{nil},
			expectedValue: "custom result",
			expectError:   false,
		},
		{
			name: "Run Effect program with error",
			program: Do(
				effectus.Effect{Verb: "TestVerb", Payload: map[string]interface{}{"id": 1}},
				func(r interface{}) *Program { return Pure(r) },
			),
			setResults:    []interface{}{"ignored"},
			setErrors:     []error{errors.New("executor error")},
			expectedValue: nil,
			expectError:   true,
		},
		{
			name: "Run sequential effects",
			program: Do(
				effectus.Effect{Verb: "First", Payload: map[string]interface{}{"id": 1}},
				func(r interface{}) *Program {
					return Do(
						effectus.Effect{Verb: "Second", Payload: map[string]interface{}{"id": 2}},
						func(r2 interface{}) *Program {
							return Pure([]string{r.(string), r2.(string)})
						},
					)
				},
			),
			setResults:    []interface{}{"result1", "result2"},
			setErrors:     []error{nil, nil},
			expectedValue: []string{"result1", "result2"},
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewMockExecutor()
			if tt.setResults != nil {
				executor.SetResults(tt.setResults, tt.setErrors)
			}

			result, err := Run(tt.program, executor)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tt.expectError && !reflect.DeepEqual(result, tt.expectedValue) {
				t.Errorf("Expected result %v but got %v", tt.expectedValue, result)
			}
		})
	}
}
