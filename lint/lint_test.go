package lint

import (
	"os"
	"testing"

	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/stretchr/testify/assert"
)

func TestLintDetectsDeadRule(t *testing.T) {
	content := `
rule "Never" priority 1 {
  when { false }
  then { Noop() }
}
`

	file := parseTempRuleFile(t, content)
	issues := LintFile(file, "test.eff", nil)

	assert.True(t, hasIssue(issues, CodeDeadRule))
}

func TestLintDetectsMissingInverse(t *testing.T) {
	content := `
rule "Freeze" priority 1 {
  when { true }
  then { FreezeAccount(accountId: "cust-1", reason: "risk") }
}
`

	file := parseTempRuleFile(t, content)

	registry := verb.NewRegistry(nil)
	assert.NoError(t, registry.RegisterVerb(&verb.Spec{
		Name:       "FreezeAccount",
		Capability: verb.CapWrite,
		ArgTypes:   map[string]string{"accountId": "string", "reason": "string"},
		ReturnType: "bool",
	}))

	issues := LintFile(file, "test.eff", registry)
	assert.True(t, hasIssue(issues, CodeMissingInverse))
}

func TestLintDetectsUnsafeExpression(t *testing.T) {
	content := `
rule "Unsafe" priority 1 {
  when { customer.email matches ".*@example.com" }
  then { Noop() }
}
`

	file := parseTempRuleFile(t, content)
	issues := LintFile(file, "test.eff", nil)
	assert.True(t, hasIssue(issues, CodeUnsafeExpression))
}

func TestLintDetectsDivisionByZero(t *testing.T) {
	content := `
rule "Divide" priority 1 {
  when { order.total / 0 > 1 }
  then { Noop() }
}
`

	file := parseTempRuleFile(t, content)
	issues := LintFile(file, "test.eff", nil)
	assert.True(t, hasIssue(issues, CodeDivisionByZero))
}

func TestLintDetectsMissingConcurrencyFlags(t *testing.T) {
	content := `
rule "Mutate" priority 1 {
  when { true }
  then { UpdateAccount(accountId: "cust-1") }
}
`

	file := parseTempRuleFile(t, content)

	registry := verb.NewRegistry(nil)
	assert.NoError(t, registry.RegisterVerb(&verb.Spec{
		Name:       "UpdateAccount",
		Capability: verb.CapWrite,
		ArgTypes:   map[string]string{"accountId": "string"},
		ReturnType: "bool",
	}))

	issues := LintFile(file, "test.eff", registry)
	assert.True(t, hasIssue(issues, CodeMissingConcurrency))
}

func TestLintDetectsOverbroadCapability(t *testing.T) {
	content := `
rule "Broad" priority 1 {
  when { true }
  then { AdminAction() }
}
`

	file := parseTempRuleFile(t, content)

	registry := verb.NewRegistry(nil)
	assert.NoError(t, registry.RegisterVerb(&verb.Spec{
		Name:       "AdminAction",
		Capability: verb.CapAll,
		ArgTypes:   map[string]string{},
		ReturnType: "bool",
	}))

	issues := LintFile(file, "test.eff", registry)
	assert.True(t, hasIssue(issues, CodeOverbroadCapability))
}

func TestLintDetectsMissingResources(t *testing.T) {
	content := `
rule "Mutate" priority 1 {
  when { true }
  then { UpdateAccount(accountId: "cust-1") }
}
`

	file := parseTempRuleFile(t, content)

	registry := verb.NewRegistry(nil)
	assert.NoError(t, registry.RegisterVerb(&verb.Spec{
		Name:       "UpdateAccount",
		Capability: verb.CapWrite,
		ArgTypes:   map[string]string{"accountId": "string"},
		ReturnType: "bool",
	}))

	issues := LintFile(file, "test.eff", registry)
	assert.True(t, hasIssue(issues, CodeMissingResources))
}

func TestLintDetectsMissingCapability(t *testing.T) {
	content := `
rule "NoCap" priority 1 {
  when { true }
  then { SideEffect(value: "ok") }
}
`

	file := parseTempRuleFile(t, content)

	registry := verb.NewRegistry(nil)
	assert.NoError(t, registry.RegisterVerb(&verb.Spec{
		Name:       "SideEffect",
		Capability: verb.CapNone,
		ArgTypes:   map[string]string{"value": "string"},
		ReturnType: "bool",
	}))

	issues := LintFile(file, "test.eff", registry)
	assert.True(t, hasIssue(issues, CodeMissingCapability))
}

func TestLintDetectsUnusedBinding(t *testing.T) {
	content := `
flow "UnusedBinding" priority 1 {
  when { true }
  steps {
    caseId = OpenCase(orderId: "ord-1")
  }
}
`

	file := parseTempRuleFile(t, content)

	registry := verb.NewRegistry(nil)
	assert.NoError(t, registry.RegisterVerb(&verb.Spec{
		Name:       "OpenCase",
		Capability: verb.CapRead,
		ArgTypes:   map[string]string{"orderId": "string"},
		ReturnType: "string",
	}))

	issues := LintFile(file, "test.effx", registry)
	assert.True(t, hasIssue(issues, CodeUnusedBinding))
}

func TestLintDetectsBindingWithoutReturn(t *testing.T) {
	content := `
flow "BindNoReturn" priority 1 {
  when { true }
  steps {
    result = NotifyRisk(orderId: "ord-1")
  }
}
`

	file := parseTempRuleFile(t, content)

	registry := verb.NewRegistry(nil)
	assert.NoError(t, registry.RegisterVerb(&verb.Spec{
		Name:       "NotifyRisk",
		Capability: verb.CapRead,
		ArgTypes:   map[string]string{"orderId": "string"},
	}))

	issues := LintFile(file, "test.effx", registry)
	assert.True(t, hasIssue(issues, CodeBindingNoReturn))
}

func parseTempRuleFile(t *testing.T, content string) *ast.File {
	t.Helper()

	file, err := os.CreateTemp("", "effectus-lint-*.eff")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(file.Name())

	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		t.Fatalf("failed to write temp file: %v", err)
	}
	_ = file.Close()

	comp := compiler.NewCompiler()
	parsed, err := comp.ParseFile(file.Name())
	if err != nil {
		t.Fatalf("failed to parse file: %v", err)
	}

	return parsed
}

func hasIssue(issues []Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
