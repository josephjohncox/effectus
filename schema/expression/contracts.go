// Package expression defines the narrow contracts used by expression clients.
// The mutable implementation remains in schema during the compatibility window.
package expression

// Registry is the data/function surface required by predicate compilation and evaluation.
type Registry interface {
	Set(string, interface{})
	Get(string) (interface{}, bool)
	RegisterFunction(string, interface{})
	EvaluateExpression(string) (interface{}, error)
}

// PredicateEvaluator evaluates a compiled predicate against a registry-owned snapshot.
type PredicateEvaluator interface {
	Evaluate(map[string]interface{}) (bool, error)
}
