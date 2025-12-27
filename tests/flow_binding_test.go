package tests

import (
	"testing"

	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/stretchr/testify/assert"
)

func TestFlowBindingTypeChecking(t *testing.T) {
	typeSystem := createTestTypeSystem()
	facts := createTestFacts(typeSystem)

	tests := []struct {
		name        string
		flowContent string
		shouldPass  bool
		errContains string
	}{
		{
			name: "valid_binding_chain",
			flowContent: `
flow "BindingPass" priority 1 {
  when {
    customer.status == "active"
  }
  steps {
    caseId = OpenCase(orderId: order.id, reason: "hold")
    UpdateCase(caseId: $caseId, status: "held")
  }
}
`,
			shouldPass: true,
		},
		{
			name: "invalid_binding_type",
			flowContent: `
flow "BindingFail" priority 1 {
  when {
    customer.status == "active"
  }
  steps {
    riskScore = ScoreRisk(orderId: order.id)
    UpdateCase(caseId: $riskScore, status: "held")
  }
}
`,
			shouldPass:  false,
			errContains: "variable $riskScore has type",
		},
		{
			name: "invalid_binding_missing",
			flowContent: `
flow "BindingMissing" priority 1 {
  when {
    customer.status == "active"
  }
  steps {
    UpdateCase(caseId: $missing, status: "held")
  }
}
`,
			shouldPass:  false,
			errContains: "unknown variable reference",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			comp := compiler.NewCompiler()
			compTS := comp.GetTypeSystem()

			for _, path := range typeSystem.GetAllFactPaths() {
				factType, _ := typeSystem.GetFactType(path)
				compTS.RegisterFactType(path, factType)
			}

			compTS.RegisterVerbType(
				"OpenCase",
				map[string]*types.Type{
					"orderId": types.NewStringType(),
					"reason":  types.NewStringType(),
				},
				types.NewStringType(),
			)
			compTS.RegisterVerbType(
				"UpdateCase",
				map[string]*types.Type{
					"caseId": types.NewStringType(),
					"status": types.NewStringType(),
				},
				types.NewBoolType(),
			)
			compTS.RegisterVerbType(
				"ScoreRisk",
				map[string]*types.Type{
					"orderId": types.NewStringType(),
				},
				types.NewIntType(),
			)

			tmpFile := createTempRuleFile(t, tc.flowContent)
			defer cleanupTempFile(tmpFile)

			_, err := comp.ParseAndTypeCheck(tmpFile, facts)
			if tc.shouldPass {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				if tc.errContains != "" {
					assert.Contains(t, err.Error(), tc.errContains)
				}
			}
		})
	}
}
