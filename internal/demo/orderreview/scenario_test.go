package orderreview

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCanonicalScenarioAssets(t *testing.T) {
	scenario, err := CanonicalScenario()
	if err != nil {
		t.Fatalf("decode scenario: %v", err)
	}
	if scenario.IdempotencyKey != "order-200-created" {
		t.Fatalf("idempotency key = %q, want %q", scenario.IdempotencyKey, "order-200-created")
	}
	if scenario.Request.Namespace != "merchant-42" || scenario.Request.Universe != "merchant-42" {
		t.Fatalf("tenant identity = (%q, %q), want %q", scenario.Request.Namespace, scenario.Request.Universe, "merchant-42")
	}
	wantOrder := Order{
		ID:        "order-200",
		Total:     2499.00,
		Currency:  "USD",
		RiskScore: 82,
	}
	if scenario.Request.Facts.Order != wantOrder {
		t.Fatalf("order = %#v, want %#v", scenario.Request.Facts.Order, wantOrder)
	}

	requestJSON, err := RequestJSON()
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	var request HTTPRequest
	if err := json.Unmarshal(requestJSON, &request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if request != scenario.Request {
		t.Fatalf("derived request = %#v, want %#v", request, scenario.Request)
	}

	wantFacts := map[string]any{
		"order": map[string]any{
			"id":         wantOrder.ID,
			"total":      wantOrder.Total,
			"currency":   wantOrder.Currency,
			"risk_score": wantOrder.RiskScore,
		},
	}
	if facts := scenario.Facts(); !reflect.DeepEqual(facts, wantFacts) {
		t.Fatalf("embedded facts = %#v, want %#v", facts, wantFacts)
	}

	ruleSource, err := RuleSource()
	if err != nil {
		t.Fatalf("read rule source: %v", err)
	}
	rule := string(ruleSource)
	for _, required := range []string{RuleName, VerbName, Reason} {
		if !strings.Contains(rule, required) {
			t.Fatalf("rule source does not contain %q", required)
		}
	}
}
