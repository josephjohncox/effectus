package eval

import (
	"testing"

	"github.com/effectus/effectus-go"
)

// Simple facts implementation for testing
type TestFacts struct {
	data map[string]interface{}
}

func (f *TestFacts) Get(path string) (interface{}, bool) {
	val, exists := f.data[path]
	return val, exists
}

func (f *TestFacts) Schema() effectus.SchemaInfo {
	// Simple schema that accepts all paths
	return &TestSchema{}
}

type TestSchema struct{}

func (s *TestSchema) ValidatePath(path string) bool {
	return true
}

func TestCompareFact(t *testing.T) {
	tests := []struct {
		name         string
		factValue    interface{}
		op           string
		literalValue interface{}
		expected     bool
	}{
		// Equality tests
		{name: "string equality - true", factValue: "test", op: "==", literalValue: "test", expected: true},
		{name: "string equality - false", factValue: "test", op: "==", literalValue: "other", expected: false},
		{name: "int equality - true", factValue: 42, op: "==", literalValue: 42, expected: true},
		{name: "int equality - false", factValue: 42, op: "==", literalValue: 43, expected: false},
		{name: "float equality - true", factValue: 3.14, op: "==", literalValue: 3.14, expected: true},
		{name: "float equality - false", factValue: 3.14, op: "==", literalValue: 3.15, expected: false},
		{name: "bool equality - true", factValue: true, op: "==", literalValue: true, expected: true},
		{name: "bool equality - false", factValue: true, op: "==", literalValue: false, expected: false},

		// Inequality tests
		{name: "string inequality - true", factValue: "test", op: "!=", literalValue: "other", expected: true},
		{name: "string inequality - false", factValue: "test", op: "!=", literalValue: "test", expected: false},
		{name: "int inequality - true", factValue: 42, op: "!=", literalValue: 43, expected: true},
		{name: "int inequality - false", factValue: 42, op: "!=", literalValue: 42, expected: false},

		// Less than tests
		{name: "int less than - true", factValue: 10, op: "<", literalValue: 20, expected: true},
		{name: "int less than - false", factValue: 20, op: "<", literalValue: 10, expected: false},
		{name: "float less than - true", factValue: 3.14, op: "<", literalValue: 3.15, expected: true},
		{name: "float less than - false", factValue: 3.15, op: "<", literalValue: 3.14, expected: false},
		{name: "mixed numeric less than - true", factValue: 5, op: "<", literalValue: 5.5, expected: true},
		{name: "mixed numeric less than - false", factValue: 5.5, op: "<", literalValue: 5, expected: false},

		// Greater than tests
		{name: "int greater than - true", factValue: 20, op: ">", literalValue: 10, expected: true},
		{name: "int greater than - false", factValue: 10, op: ">", literalValue: 20, expected: false},
		{name: "float greater than - true", factValue: 3.15, op: ">", literalValue: 3.14, expected: true},
		{name: "float greater than - false", factValue: 3.14, op: ">", literalValue: 3.15, expected: false},

		// Less than or equal tests
		{name: "int less than or equal - true (less)", factValue: 10, op: "<=", literalValue: 20, expected: true},
		{name: "int less than or equal - true (equal)", factValue: 10, op: "<=", literalValue: 10, expected: true},
		{name: "int less than or equal - false", factValue: 20, op: "<=", literalValue: 10, expected: false},

		// Greater than or equal tests
		{name: "int greater than or equal - true (greater)", factValue: 20, op: ">=", literalValue: 10, expected: true},
		{name: "int greater than or equal - true (equal)", factValue: 10, op: ">=", literalValue: 10, expected: true},
		{name: "int greater than or equal - false", factValue: 10, op: ">=", literalValue: 20, expected: false},

		// Contains tests
		{name: "string contains - invalid operation", factValue: "test", op: "contains", literalValue: "e", expected: true},
		{name: "array contains - invalid operation", factValue: []interface{}{"a", "b", "c"}, op: "contains", literalValue: "b", expected: true},

		// In tests
		{name: "in - invalid operation", factValue: "b", op: "in", literalValue: []interface{}{"a", "b", "c"}, expected: true},

		// Unknown operator
		{name: "unknown operator", factValue: "test", op: "unknown", literalValue: "test", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompareFact(tt.factValue, tt.op, tt.literalValue)
			if result != tt.expected {
				t.Errorf("CompareFact(%v, %s, %v) = %v, want %v",
					tt.factValue, tt.op, tt.literalValue, result, tt.expected)
			}
		})
	}
}

func TestEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        interface{}
		b        interface{}
		expected bool
	}{
		{name: "string equal", a: "test", b: "test", expected: true},
		{name: "string unequal", a: "test", b: "other", expected: false},
		{name: "quoted string equal", a: "test", b: `"test"`, expected: true},
		{name: "int equal", a: 42, b: 42, expected: true},
		{name: "int unequal", a: 42, b: 43, expected: false},
		{name: "float equal", a: 3.14, b: 3.14, expected: true},
		{name: "float unequal", a: 3.14, b: 3.15, expected: false},
		{name: "mixed numeric equal", a: 5, b: 5.0, expected: true},
		{name: "bool equal", a: true, b: true, expected: true},
		{name: "bool unequal", a: true, b: false, expected: false},
		{name: "different types", a: "5", b: 5, expected: true},
		{name: "nil comparison", a: nil, b: nil, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Equal(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Equal(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestLessThan(t *testing.T) {
	tests := []struct {
		name     string
		a        interface{}
		b        interface{}
		expected bool
	}{
		{name: "int less", a: 5, b: 10, expected: true},
		{name: "int not less", a: 10, b: 5, expected: false},
		{name: "int equal", a: 5, b: 5, expected: false},
		{name: "float less", a: 3.14, b: 3.15, expected: true},
		{name: "float not less", a: 3.15, b: 3.14, expected: false},
		{name: "mixed numeric less", a: 5, b: 5.1, expected: true},
		{name: "mixed numeric not less", a: 5.1, b: 5, expected: false},
		{name: "string less", a: "abc", b: "def", expected: true},
		{name: "string not less", a: "def", b: "abc", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := LessThan(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("LessThan(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestGreaterThan(t *testing.T) {
	tests := []struct {
		name     string
		a        interface{}
		b        interface{}
		expected bool
	}{
		{name: "int greater", a: 10, b: 5, expected: true},
		{name: "int not greater", a: 5, b: 10, expected: false},
		{name: "int equal", a: 5, b: 5, expected: false},
		{name: "float greater", a: 3.15, b: 3.14, expected: true},
		{name: "float not greater", a: 3.14, b: 3.15, expected: false},
		{name: "mixed numeric greater", a: 5.1, b: 5, expected: true},
		{name: "mixed numeric not greater", a: 5, b: 5.1, expected: false},
		{name: "string greater", a: "def", b: "abc", expected: true},
		{name: "string not greater", a: "abc", b: "def", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GreaterThan(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("GreaterThan(%v, %v) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name      string
		container interface{}
		item      interface{}
		expected  bool
	}{
		{name: "slice contains string", container: []interface{}{"a", "b", "c"}, item: "b", expected: true},
		{name: "slice does not contain string", container: []interface{}{"a", "b", "c"}, item: "d", expected: false},
		{name: "slice contains int", container: []interface{}{1, 2, 3}, item: 2, expected: true},
		{name: "slice does not contain int", container: []interface{}{1, 2, 3}, item: 4, expected: false},
		{name: "slice with mixed types", container: []interface{}{1, "b", true}, item: "b", expected: true},
		{name: "string equality check", container: "test", item: "test", expected: true},
		{name: "string inequality check", container: "test", item: "other", expected: false},
		{name: "non-container type", container: 42, item: 2, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Contains(tt.container, tt.item)
			if result != tt.expected {
				t.Errorf("Contains(%v, %v) = %v, want %v", tt.container, tt.item, result, tt.expected)
			}
		})
	}
}

func TestEvaluatePredicates(t *testing.T) {
	facts := &TestFacts{
		data: map[string]interface{}{
			"customer.id":     "C123",
			"customer.name":   "Acme Corp",
			"customer.active": true,
			"order.total":     250.50,
			"order.items":     3,
		},
	}

	tests := []struct {
		name       string
		predicates []*Predicate
		expected   bool
	}{
		{
			name: "Single predicate - true",
			predicates: []*Predicate{
				{Path: "customer.id", Op: "==", Lit: "C123"},
			},
			expected: true,
		},
		{
			name: "Single predicate - false",
			predicates: []*Predicate{
				{Path: "customer.id", Op: "==", Lit: "C124"},
			},
			expected: false,
		},
		{
			name: "Multiple predicates - all true",
			predicates: []*Predicate{
				{Path: "customer.id", Op: "==", Lit: "C123"},
				{Path: "customer.active", Op: "==", Lit: true},
				{Path: "order.total", Op: ">", Lit: 100.0},
			},
			expected: true,
		},
		{
			name: "Multiple predicates - one false",
			predicates: []*Predicate{
				{Path: "customer.id", Op: "==", Lit: "C123"},
				{Path: "order.total", Op: "<", Lit: 100.0},
			},
			expected: false,
		},
		{
			name: "Path doesn't exist",
			predicates: []*Predicate{
				{Path: "customer.nonexistent", Op: "==", Lit: "value"},
			},
			expected: false,
		},
		{
			name:       "Empty predicates",
			predicates: []*Predicate{},
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvaluatePredicates(tt.predicates, facts)
			if result != tt.expected {
				t.Errorf("EvaluatePredicates() = %v, want %v", result, tt.expected)
			}
		})
	}
}
