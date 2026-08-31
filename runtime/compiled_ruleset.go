package runtime

import "github.com/josephjohncox/effectus/compiler"

// CompiledRuleset is retained as the storage representation used by legacy
// ruleset persistence. It is not a gRPC method-registration API.
type CompiledRuleset struct {
	Name          string
	Version       string
	Description   string
	FactSchema    *Schema
	EffectSchemas map[string]*Schema
	Rules         []CompiledRule
	Verbs         map[string]*compiler.CompiledVerbSpec
	Dependencies  []string
	Capabilities  []string
	Metadata      map[string]string
}

type CompiledRule struct {
	Name        string
	Type        RuleType
	Predicates  []CompiledPredicate
	Effects     []CompiledEffect
	Priority    int
	Description string
}

type RuleType string

const (
	RuleTypeList RuleType = "list"
	RuleTypeFlow RuleType = "flow"
)

type CompiledPredicate struct {
	Path     string
	Operator string
	Value    any
}
type CompiledEffect struct {
	VerbName string
	Args     map[string]any
}
type Schema struct {
	Name        string
	Fields      map[string]*FieldType
	Required    []string
	Description string
}
type FieldType struct {
	Type        string
	MessageType string
	Required    bool
	Description string
}
