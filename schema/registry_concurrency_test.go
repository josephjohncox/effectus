package schema

import (
	"sync"
	"testing"

	effectus "github.com/josephjohncox/effectus"
	"github.com/stretchr/testify/require"
)

type predicateTestFacts map[string]interface{}

func (f predicateTestFacts) Get(path string) (interface{}, bool) {
	if path == "" {
		return map[string]interface{}(f), true
	}
	value, ok := f[path]
	return value, ok
}

func (predicateTestFacts) Schema() effectus.SchemaInfo { return predicateTestSchema{} }

type predicateTestSchema struct{}

func (predicateTestSchema) ValidatePath(string) bool { return true }

func TestRegistryConcurrentMutationAndPredicateEvaluation(t *testing.T) {
	registry := NewRegistry()
	registry.Set("enabled", true)
	predicate, err := registry.NewPredicate("enabled")
	require.NoError(t, err)

	var wait sync.WaitGroup
	for i := 0; i < 100; i++ {
		wait.Add(3)
		go func(value int) {
			defer wait.Done()
			registry.Set("counter", value)
		}(i)
		go func(value int) {
			defer wait.Done()
			registry.RegisterFunction("dynamic", func() int { return value })
		}(i)
		go func() {
			defer wait.Done()
			matched, evalErr := predicate.EvaluateWithRegistry(registry)
			if evalErr != nil || !matched {
				t.Errorf("evaluate: matched=%v err=%v", matched, evalErr)
			}
		}()
	}
	wait.Wait()
}

func TestEvaluatePredicatesWithFactsDoesNotMutateCompiledPredicate(t *testing.T) {
	registry := NewRegistry()
	registry.Set("enabled", true)
	predicate, err := registry.NewPredicate("enabled")
	require.NoError(t, err)

	const iterations = 100
	failures := make(chan string, iterations*2)
	var wait sync.WaitGroup
	for i := 0; i < iterations; i++ {
		wait.Add(2)
		go func() {
			defer wait.Done()
			matched, err := EvaluatePredicatesWithFactsE([]*Predicate{predicate}, predicateTestFacts{"enabled": true})
			if err != nil || !matched {
				failures <- "true facts did not match"
			}
		}()
		go func() {
			defer wait.Done()
			matched, err := EvaluatePredicatesWithFactsE([]*Predicate{predicate}, predicateTestFacts{"enabled": false})
			if err != nil || matched {
				failures <- "false facts matched"
			}
		}()
	}
	wait.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
}
