//go:build integration
// +build integration

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	effectusruntime "github.com/josephjohncox/effectus/runtime"
	"github.com/josephjohncox/effectus/schema/types"
	"github.com/josephjohncox/effectus/schema/verb"
	"github.com/josephjohncox/effectus/unified"
	"github.com/stretchr/testify/require"
)

func TestHotloadFailsClosedWithCheckedEngine(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	schemaPath := filepath.Join(repoRoot, "examples", "fraud_e2e", "schema", "fraud_facts.json")
	rulePath := filepath.Join(repoRoot, "examples", "fraud_e2e", "rules", "fraud_rules.eff")

	ruleContent, err := os.ReadFile(rulePath)
	require.NoError(t, err)

	typeSystem := types.NewTypeSystem()
	require.NoError(t, typeSystem.LoadSchemaFile(schemaPath))

	verbReg := verb.NewRegistry(typeSystem)
	registerDemoVerbs(t, verbReg)

	bundle := &unified.Bundle{
		Name:      "integration-demo",
		Version:   "1.0.0",
		FactTypes: unified.SummarizeFactTypes(typeSystem),
	}

	auth, err := buildAPIAuth("disabled", "", "")
	require.NoError(t, err)

	state := newServerState(bundle, nil, nil, factStoreConfig{}, auth, nil, nil, typeSystem, nil, verbReg, true, nil, false, nil, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/rules/validate", state.handleRuleValidate)
	mux.HandleFunc("/api/rules/hotload", state.handleRuleHotload)

	server := httptest.NewServer(state.withAPIMiddleware(mux))
	defer server.Close()

	payload := map[string]interface{}{
		"path":    "rules/fraud_rules.eff",
		"format":  "eff",
		"content": string(ruleContent),
		"replace": true,
	}

	validateResp := postRuleCheck(t, server.URL+"/api/rules/validate", payload)
	require.True(t, validateResp.OK, "validate diagnostics: %+v", validateResp.Diagnostics)

	hotloadResp := postRuleCheck(t, server.URL+"/api/rules/hotload", payload)
	require.True(t, hotloadResp.OK, "hotload diagnostics: %+v", hotloadResp.Diagnostics)
	require.True(t, hotloadResp.Applied)

	updated := state.Bundle()
	require.NotNil(t, updated)
	require.NotEmpty(t, updated.Rules)

	state.SetCheckedEngine(new(effectusruntime.Engine))
	checkedResponse := postRuleCheck(t, server.URL+"/api/rules/hotload", payload)
	require.False(t, checkedResponse.Applied)
	require.NotEmpty(t, checkedResponse.Diagnostics)
	require.Contains(t, checkedResponse.Diagnostics[0].Message, "checked execution engine is installed")
}

func registerDemoVerbs(t *testing.T, registry *verb.Registry) {
	t.Helper()
	if registry == nil {
		t.Fatal("verb registry is nil")
	}

	register := func(name string, args map[string]string) {
		spec := verb.NewSpec(name, verb.CapWrite|verb.CapIdempotent, args, "bool").
			WithInverse(name + "Undo").
			WithResources(verb.ResourceSet{
				{Resource: "fraud", Cap: verb.CapWrite | verb.CapIdempotent},
			})
		require.NoError(t, registry.RegisterVerb(spec))
		undo := verb.NewSpec(name+"Undo", verb.CapWrite|verb.CapIdempotent, args, "bool").
			WithResources(verb.ResourceSet{{Resource: "fraud", Cap: verb.CapWrite | verb.CapIdempotent}})
		require.NoError(t, registry.RegisterVerb(undo))
	}

	register("FlagFraud", map[string]string{"orderId": "string", "reason": "string"})
	register("NotifyRisk", map[string]string{"orderId": "string", "channel": "string"})
	register("FreezeAccount", map[string]string{"accountId": "string", "reason": "string"})
}

func postRuleCheck(t *testing.T, url string, payload map[string]interface{}) ruleCheckResponse {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Contains(t, []int{http.StatusOK, http.StatusUnprocessableEntity, http.StatusConflict}, resp.StatusCode)

	var parsed ruleCheckResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&parsed))
	return parsed
}
