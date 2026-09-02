// Package orderreview contains test-only helpers for the shared first-run scenario.
package orderreview

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	RuleName = "ReviewLargeOrder"
	VerbName = "RequestManualReview"
	Reason   = "value_or_risk"
)

// Scenario is the canonical input shared by both first-run paths.
type Scenario struct {
	IdempotencyKey string      `json:"idempotency_key"`
	Request        HTTPRequest `json:"request"`
}

// HTTPRequest is the request body submitted to the facts API.
type HTTPRequest struct {
	Namespace string        `json:"namespace"`
	Universe  string        `json:"universe"`
	Facts     ScenarioFacts `json:"facts"`
}

// ScenarioFacts contains the facts supplied by the order-review scenario.
type ScenarioFacts struct {
	Order Order `json:"order"`
}

// Order contains the order facts used by the shared rule.
type Order struct {
	ID        string  `json:"id"`
	Total     float64 `json:"total"`
	Currency  string  `json:"currency"`
	RiskScore int64   `json:"risk_score"`
}

// RuleSource reads the shared rule artifact. It is a demo helper, not a
// production bundle API; callers must run from the repository root or examples.
func RuleSource() ([]byte, error) {
	return readScenarioAsset(filepath.Join("rules", "order_review.eff"))
}

// CanonicalScenario decodes the shared scenario artifact.
func CanonicalScenario() (Scenario, error) {
	data, err := readScenarioAsset(filepath.Join("data", "order.json"))
	if err != nil {
		return Scenario{}, err
	}
	var scenario Scenario
	if err := json.Unmarshal(data, &scenario); err != nil {
		return Scenario{}, fmt.Errorf("decode canonical order-review scenario: %w", err)
	}
	return scenario, nil
}

// RequestJSON returns the canonical HTTP request derived from the scenario artifact.
func RequestJSON() ([]byte, error) {
	scenario, err := CanonicalScenario()
	if err != nil {
		return nil, err
	}
	request, err := json.Marshal(scenario.Request)
	if err != nil {
		return nil, fmt.Errorf("encode canonical order-review request: %w", err)
	}
	return request, nil
}

// Facts returns fresh nested facts derived from the scenario artifact.
func (scenario Scenario) Facts() map[string]any {
	return map[string]any{
		"order": map[string]any{
			"id":         scenario.Request.Facts.Order.ID,
			"total":      scenario.Request.Facts.Order.Total,
			"currency":   scenario.Request.Facts.Order.Currency,
			"risk_score": scenario.Request.Facts.Order.RiskScore,
		},
	}
}

func readScenarioAsset(relative string) ([]byte, error) {
	roots := []string{"examples/order_review", "order_review"}
	if _, sourceFile, _, ok := runtime.Caller(0); ok {
		roots = append(roots, filepath.Join(filepath.Dir(sourceFile), "..", "..", "..", "examples", "order_review"))
	}
	for _, root := range roots {
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err == nil {
			return data, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read order-review asset %q: %w", relative, err)
		}
	}
	return nil, fmt.Errorf("read order-review asset %q: run from the repository root or examples directory", relative)
}
