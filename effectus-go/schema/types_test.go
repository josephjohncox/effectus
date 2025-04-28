package schema

import (
	"testing"

	"github.com/alecthomas/participle/v2/lexer"
	"github.com/effectus/effectus-go/ast"
)

func TestTypeSystem(t *testing.T) {
	ts := NewTypeSystem()

	// Register some fact types
	ts.RegisterFactType("customer.id", &Type{PrimType: TypeString})
	ts.RegisterFactType("customer.email", &Type{PrimType: TypeString})
	ts.RegisterFactType("order.total", &Type{PrimType: TypeFloat})
	ts.RegisterFactType("order.items", &Type{
		PrimType: TypeList,
		ListType: &Type{
			Name:     "OrderItem",
			PrimType: TypeUnknown,
		},
	})

	// Register a verb
	argTypes := map[string]*Type{
		"to":      &Type{PrimType: TypeString},
		"subject": &Type{PrimType: TypeString},
		"body":    &Type{PrimType: TypeString},
	}
	returnType := &Type{PrimType: TypeBool}
	ts.RegisterVerbType("SendEmail", argTypes, returnType)

	// Check if types were registered correctly
	if typ, exists := ts.GetFactType("customer.id"); !exists || typ.PrimType != TypeString {
		t.Errorf("Failed to register or retrieve customer.id type")
	}

	// Test verb type registration
	verbInfo, exists := ts.VerbTypes["SendEmail"]
	if !exists {
		t.Fatalf("Failed to register SendEmail verb")
	}
	if verbInfo.ReturnType.PrimType != TypeBool {
		t.Errorf("SendEmail return type is incorrect")
	}
	if argType, exists := verbInfo.ArgTypes["to"]; !exists || argType.PrimType != TypeString {
		t.Errorf("SendEmail 'to' argument type is incorrect")
	}
}

func TestTypeInference(t *testing.T) {
	ts := NewTypeSystem()

	// Create a facts object with some data
	data := map[string]interface{}{
		"customer": map[string]interface{}{
			"id":    "12345",
			"email": "customer@example.com",
			"age":   30,
		},
		"order": map[string]interface{}{
			"total": 99.99,
			"items": []interface{}{
				map[string]interface{}{
					"name":     "Product 1",
					"quantity": 2,
					"price":    49.99,
				},
			},
		},
	}
	schema := &SimpleSchema{}
	facts := NewSimpleFacts(data, schema)

	// Infer types from a fact value
	if val, exists := facts.Get("customer.id"); exists {
		typ := ts.InferFactType("customer.id", val)
		if typ.PrimType != TypeString {
			t.Errorf("Failed to infer customer.id as string, got %v", typ.PrimType)
		}
	} else {
		t.Errorf("Failed to get customer.id from facts")
	}

	if val, exists := facts.Get("customer.age"); exists {
		typ := ts.InferFactType("customer.age", val)
		if typ.PrimType != TypeInt {
			t.Errorf("Failed to infer customer.age as int, got %v", typ.PrimType)
		}
	} else {
		t.Errorf("Failed to get customer.age from facts")
	}

	if val, exists := facts.Get("order.total"); exists {
		typ := ts.InferFactType("order.total", val)
		if typ.PrimType != TypeFloat {
			t.Errorf("Failed to infer order.total as float, got %v", typ.PrimType)
		}
	} else {
		t.Errorf("Failed to get order.total from facts")
	}

	// Check if these types can now be retrieved
	if typ, exists := ts.GetFactType("customer.id"); !exists || typ.PrimType != TypeString {
		t.Errorf("Failed to store or retrieve inferred customer.id type")
	}
}

func TestTypeCheckPredicate(t *testing.T) {
	ts := NewTypeSystem()

	// Register some fact types
	ts.RegisterFactType("customer.id", &Type{PrimType: TypeString})
	ts.RegisterFactType("order.total", &Type{PrimType: TypeFloat})
	ts.RegisterFactType("customer.active", &Type{PrimType: TypeBool})
	ts.RegisterFactType("order.items", &Type{
		PrimType: TypeList,
		ListType: &Type{
			Name:     "OrderItem",
			PrimType: TypeUnknown,
		},
	})

	// Valid predicates
	testCases := []struct {
		name      string
		predicate *ast.Predicate
		shouldErr bool
	}{
		{
			name: "string equality",
			predicate: &ast.Predicate{
				Path: "customer.id",
				Op:   "==",
				Lit: ast.Literal{
					String: func() *string { s := "12345"; return &s }(),
				},
			},
			shouldErr: false,
		},
		{
			name: "numeric comparison",
			predicate: &ast.Predicate{
				Path: "order.total",
				Op:   ">",
				Lit: ast.Literal{
					Float: func() *float64 { f := 50.0; return &f }(),
				},
			},
			shouldErr: false,
		},
		{
			name: "invalid operator for string",
			predicate: &ast.Predicate{
				Path: "customer.id",
				Op:   "<",
				Lit: ast.Literal{
					String: func() *string { s := "12345"; return &s }(),
				},
			},
			shouldErr: true,
		},
		{
			name: "contains with list",
			predicate: &ast.Predicate{
				Path: "order.items",
				Op:   "contains",
				Lit: ast.Literal{
					Map: []*ast.MapEntry{
						{
							Key: "name",
							Value: ast.Literal{
								String: func() *string { s := "Product 1"; return &s }(),
							},
						},
					},
				},
			},
			shouldErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ts.TypeCheckPredicate(tc.predicate)
			if tc.shouldErr && err == nil {
				t.Errorf("Expected error but got none")
			} else if !tc.shouldErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestTypeCheckEffect(t *testing.T) {
	ts := NewTypeSystem()

	// Register some fact types
	ts.RegisterFactType("customer.email", &Type{PrimType: TypeString})

	// Register a verb
	argTypes := map[string]*Type{
		"to":      &Type{PrimType: TypeString},
		"subject": &Type{PrimType: TypeString},
		"body":    &Type{PrimType: TypeString},
	}
	returnType := &Type{PrimType: TypeBool}
	ts.RegisterVerbType("SendEmail", argTypes, returnType)

	// Valid effect
	validEffect := &ast.Effect{
		Verb: "SendEmail",
		Args: []*ast.StepArg{
			{
				Name: "to",
				Value: &ast.ArgValue{
					FactPath: "customer.email",
				},
			},
			{
				Name: "subject",
				Value: &ast.ArgValue{
					Literal: &ast.Literal{
						String: func() *string { s := "Hello"; return &s }(),
					},
				},
			},
			{
				Name: "body",
				Value: &ast.ArgValue{
					Literal: &ast.Literal{
						String: func() *string { s := "This is a test"; return &s }(),
					},
				},
			},
		},
	}

	// Invalid effect (wrong arg type)
	invalidEffect := &ast.Effect{
		Verb: "SendEmail",
		Args: []*ast.StepArg{
			{
				Name: "to",
				Value: &ast.ArgValue{
					Literal: &ast.Literal{
						Int: func() *int { i := 12345; return &i }(),
					},
				},
			},
		},
	}

	// Test valid effect
	if err := ts.TypeCheckEffect(validEffect, nil); err != nil {
		t.Errorf("Valid effect failed type check: %v", err)
	}

	// Test invalid effect
	if err := ts.TypeCheckEffect(invalidEffect, nil); err == nil {
		t.Errorf("Invalid effect passed type check")
	}
}

func TestTypeCheckRuleAndFlow(t *testing.T) {
	ts := NewTypeSystem()

	// Register some fact types
	ts.RegisterFactType("customer.id", &Type{PrimType: TypeString})
	ts.RegisterFactType("customer.email", &Type{PrimType: TypeString})
	ts.RegisterFactType("order.total", &Type{PrimType: TypeFloat})
	ts.RegisterFactType("order.items", &Type{
		PrimType: TypeList,
		ListType: &Type{
			Name:     "OrderItem",
			PrimType: TypeUnknown,
		},
	})

	// Register verbs
	ts.RegisterVerbType("SendEmail", map[string]*Type{
		"to":      &Type{PrimType: TypeString},
		"subject": &Type{PrimType: TypeString},
		"body":    &Type{PrimType: TypeString},
	}, &Type{PrimType: TypeBool})

	ts.RegisterVerbType("LogOrder", map[string]*Type{
		"order_id": &Type{PrimType: TypeString},
		"total":    &Type{PrimType: TypeFloat},
	}, &Type{PrimType: TypeBool})

	// Create a test rule
	rule := &ast.Rule{
		Pos:      lexer.Position{},
		Name:     "Test Rule",
		Priority: 1,
		When: &ast.PredicateBlock{
			Predicates: []*ast.Predicate{
				{
					Path: "order.total",
					Op:   ">",
					Lit: ast.Literal{
						Float: func() *float64 { f := 100.0; return &f }(),
					},
				},
			},
		},
		Then: &ast.EffectBlock{
			Effects: []*ast.Effect{
				{
					Verb: "SendEmail",
					Args: []*ast.StepArg{
						{
							Name: "to",
							Value: &ast.ArgValue{
								FactPath: "customer.email",
							},
						},
						{
							Name: "subject",
							Value: &ast.ArgValue{
								Literal: &ast.Literal{
									String: func() *string { s := "High Value Order"; return &s }(),
								},
							},
						},
						{
							Name: "body",
							Value: &ast.ArgValue{
								Literal: &ast.Literal{
									String: func() *string { s := "You placed a high value order"; return &s }(),
								},
							},
						},
					},
				},
			},
		},
	}

	// Create a test flow
	flow := &ast.Flow{
		Pos:      lexer.Position{},
		Name:     "Test Flow",
		Priority: 1,
		When: &ast.PredicateBlock{
			Predicates: []*ast.Predicate{
				{
					Path: "order.total",
					Op:   ">",
					Lit: ast.Literal{
						Float: func() *float64 { f := 100.0; return &f }(),
					},
				},
			},
		},
		Steps: &ast.StepBlock{
			Steps: []*ast.Step{
				{
					Verb: "LogOrder",
					Args: []*ast.StepArg{
						{
							Name: "order_id",
							Value: &ast.ArgValue{
								FactPath: "customer.id",
							},
						},
						{
							Name: "total",
							Value: &ast.ArgValue{
								FactPath: "order.total",
							},
						},
					},
					BindName: "logResult",
				},
				{
					Verb: "SendEmail",
					Args: []*ast.StepArg{
						{
							Name: "to",
							Value: &ast.ArgValue{
								FactPath: "customer.email",
							},
						},
						{
							Name: "subject",
							Value: &ast.ArgValue{
								Literal: &ast.Literal{
									String: func() *string { s := "Order Logged"; return &s }(),
								},
							},
						},
						{
							Name: "body",
							Value: &ast.ArgValue{
								Literal: &ast.Literal{
									String: func() *string { s := "Your order has been logged"; return &s }(),
								},
							},
						},
					},
				},
			},
		},
	}

	// Test rule type checking
	if err := ts.TypeCheckRule(rule); err != nil {
		t.Errorf("Valid rule failed type check: %v", err)
	}

	// Test flow type checking
	if err := ts.TypeCheckFlow(flow); err != nil {
		t.Errorf("Valid flow failed type check: %v", err)
	}

	// Create a file with both
	file := &ast.File{
		Rules: []*ast.Rule{rule},
		Flows: []*ast.Flow{flow},
	}

	// Test file type checking
	if err := ts.TypeCheckFile(file); err != nil {
		t.Errorf("Valid file failed type check: %v", err)
	}
}
