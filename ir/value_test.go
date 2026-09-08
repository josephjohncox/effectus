package ir_test

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/josephjohncox/effectus/ir"
	"github.com/stretchr/testify/require"
)

func TestNormalizeValueClosedCollections(t *testing.T) {
	env := ir.Environment{Types: map[string]ir.TypeDefinition{
		"Tags":    {Kind: ir.TypeKindList, ElementType: "string"},
		"Scores":  {Kind: ir.TypeKindMap, ElementType: "int"},
		"Profile": {Kind: ir.TypeKindObject, Fields: map[string]string{"tags": "Tags", "scores": "Scores", "blob": "bytes"}, RequiredFields: []string{"tags", "scores"}},
	}}
	for _, name := range []string{"list<string>", "[]string", "Tags"} {
		result, err := ir.NormalizeValue(env, name, []string{"a", "b"})
		require.NoError(t, err)
		require.Equal(t, []any{"a", "b"}, result)
		_, err = ir.NormalizeValue(env, name, []any{"ok", 7})
		require.ErrorContains(t, err, "list item 1")
		result, err = ir.NormalizeValue(env, name, []string(nil))
		require.NoError(t, err)
		require.Equal(t, []any{}, result)
	}
	for _, name := range []string{"map<int>", "Scores"} {
		result, err := ir.NormalizeValue(env, name, map[string]int{"a": 1})
		require.NoError(t, err)
		require.Equal(t, map[string]any{"a": int64(1)}, result)
		_, err = ir.NormalizeValue(env, name, map[string]any{"a": "bad"})
		require.ErrorContains(t, err, `field "a"`)
		_, err = ir.NormalizeValue(env, name, map[int]int{1: 1})
		require.Error(t, err)
	}
	input := map[string]any{"tags": []string{"vip"}, "scores": map[string]int{"a": 1}, "blob": []byte{1, 2}}
	result, err := ir.NormalizeValue(env, "Profile", input)
	require.NoError(t, err)
	require.Equal(t, "AQI=", result.(map[string]any)["blob"])
	result.(map[string]any)["tags"].([]any)[0] = "changed"
	require.Equal(t, "vip", input["tags"].([]string)[0])
	for _, test := range []struct {
		value any
		part  string
	}{
		{map[string]any{"tags": []string{}}, `missing required field "scores"`},
		{map[string]any{"extra": true}, `unknown field "extra"`},
		{map[string]any{"tags": []any{"ok", 1}, "scores": map[string]int{}}, `field "tags": list item 1`},
		{map[string]any{"tags": []string{}, "scores": map[string]any{"a": false}}, `field "scores": field "a"`},
	} {
		_, err := ir.NormalizeValue(env, "Profile", test.value)
		require.ErrorContains(t, err, test.part)
	}
	result, err = ir.NormalizeValue(env, "list<int>", []byte{1, 2})
	require.NoError(t, err)
	require.Equal(t, []any{int64(1), int64(2)}, result)
	_, err = ir.NormalizeValue(env, "list<map<Profile>>", []any{map[string]any{"p": map[string]any{"tags": []any{false}}}})
	require.ErrorContains(t, err, `list item 0: field "p": field "tags": list item 0`)
}

func TestNormalizeValueScalarBoundariesAndJSON(t *testing.T) {
	for _, test := range []struct {
		name     string
		value    any
		expected any
		valid    bool
	}{
		{"int", json.Number("9223372036854775807"), int64(math.MaxInt64), true},
		{"int", json.Number("-9223372036854775808"), int64(math.MinInt64), true},
		{"int", json.Number("9223372036854775807.0"), int64(math.MaxInt64), true},
		{"int", json.Number("9.223372036854775807e18"), int64(math.MaxInt64), true},
		{"int", json.Number("9223372036854775808"), nil, false},
		{"int", json.Number("-9223372036854775809"), nil, false},
		{"int", float64(math.MaxInt64), nil, false},
		{"int", math.Nextafter(float64(math.MinInt64), math.Inf(-1)), nil, false},
		{"int", uint64(math.MaxInt64) + 1, nil, false},
		{"int", json.Number("1.5"), nil, false},
		{"int", json.Number("1e999999999"), nil, false},
		{"int", json.Number("01"), nil, false},
		{"int", math.Inf(1), nil, false},
		{"int", math.NaN(), nil, false},
		{"int", float64(42), int64(42), true},
		{"float", uint16(42), float64(42), true},
		{"float", json.Number("1e309"), nil, false},
		{"float", float32(math.Inf(1)), nil, false},
		{"float", json.Number("1.25"), 1.25, true},
		{"bytes", []byte{0, 255}, "AP8=", true},
		{"bytes", "AP8=", "AP8=", true},
		{"bytes", []byte(nil), "", true},
		{"bytes", "AP8", nil, false},
		{"bytes", "AP9=", nil, false},
		{"bytes", "AP8=\n", nil, false},
		{"null", nil, nil, true},
		{"string", nil, nil, false},
	} {
		t.Run(test.name+"/"+strings.ReplaceAll(toJSON(test.value), "/", "_"), func(t *testing.T) {
			value, err := ir.NormalizeValue(ir.Environment{}, test.name, test.value)
			if !test.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expected, value)
			raw, err := json.Marshal(value)
			require.NoError(t, err)
			decoder := json.NewDecoder(strings.NewReader(string(raw)))
			decoder.UseNumber()
			var decoded any
			require.NoError(t, decoder.Decode(&decoded))
			again, err := ir.NormalizeValue(ir.Environment{}, test.name, decoded)
			require.NoError(t, err)
			require.Equal(t, value, again)
		})
	}
}
func toJSON(value any) string { b, _ := json.Marshal(value); return string(b) }

func TestNormalizeValueBoundsCyclesAndMalformedTypes(t *testing.T) {
	env := ir.Environment{Types: map[string]ir.TypeDefinition{"Node": {Kind: ir.TypeKindObject, Fields: map[string]string{"next": "Node"}}}}
	cycle := map[string]any{}
	cycle["next"] = cycle
	_, err := ir.NormalizeValue(env, "Node", cycle)
	require.ErrorContains(t, err, "depth")
	_, err = ir.NormalizeValue(ir.Environment{}, strings.Repeat("list<", 66)+"int"+strings.Repeat(">", 66), []any{})
	require.ErrorContains(t, err, "type depth")
	env.Types["Bad"] = ir.TypeDefinition{Kind: ir.TypeKindList, ElementType: "missing"}
	_, err = ir.NormalizeValue(env, "Bad", []any{})
	require.ErrorContains(t, err, "unknown type")
}

func TestValidationDefaultsIgnoreGlobalMutationAndReturnCopies(t *testing.T) {
	env := testEnvironment(t)
	artifact := validArtifact(t, env)
	checked, err := ir.Check(artifact, env, ir.Limits{})
	require.NoError(t, err)
	saved := ir.DefaultLimits
	defer func() { ir.DefaultLimits = saved }()
	defaults := ir.ValidationDefaults()
	defaults.MaxPlans = 1
	require.Greater(t, ir.ValidationDefaults().MaxPlans, 1)
	// Only the writer touches the deprecated global; checking must not read it.
	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Wait()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			ir.DefaultLimits = ir.Limits{MaxPlans: 1, MaxArtifactBytes: 1}
		}
	}()
	for i := 0; i < 100; i++ {
		_, err := ir.Check(artifact, env, ir.Limits{})
		require.NoError(t, err)
		_, err = ir.Parse(checked.Marshal(), env, ir.Limits{})
		require.NoError(t, err)
	}
}

func TestCheckAndParseRejectEveryNegativeLimit(t *testing.T) {
	env := testEnvironment(t)
	artifact := validArtifact(t, env)
	checked, err := ir.Check(artifact, env, ir.Limits{})
	require.NoError(t, err)
	for i := 0; i < reflect.TypeOf(ir.Limits{}).NumField(); i++ {
		limits := ir.Limits{}
		rv := reflect.ValueOf(&limits).Elem()
		rv.Field(i).SetInt(-1)
		field := rv.Type().Field(i).Name
		t.Run(field, func(t *testing.T) {
			_, err := ir.Check(artifact, env, limits)
			require.ErrorIs(t, err, ir.ErrInvalidArtifact)
			require.ErrorContains(t, err, field)
			_, err = ir.Parse(checked.Marshal(), env, limits)
			require.ErrorIs(t, err, ir.ErrInvalidArtifact)
			require.ErrorContains(t, err, field)
		})
	}
}
