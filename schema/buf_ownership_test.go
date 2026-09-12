package schema

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBufRegistryOwnsAllMutableSchemaData(t *testing.T) {
	b, err := NewBufIntegration(t.TempDir())
	require.NoError(t, err)
	defaultValue := map[string]any{"values": []any{int64(9007199254740993), []string{"original"}}, "bytes": []byte{1, 2}}
	verb := &VerbSchema{Name: "notify", InputSchema: map[string]any{"message": map[string]any{"type": "string", "default": defaultValue}}, OutputSchema: map[string]any{"sent": "boolean"}, RequiredCapabilities: []string{"send"}}
	fact := &FactSchema{Name: "order", Schema: map[string]any{"id": "string"}, Indexes: []IndexDefinition{{Name: "by_id", Fields: []string{"id"}, Options: map[string]string{"a": "b"}}}, RetentionPolicy: &RetentionPolicy{Conditions: map[string]string{"a": "b"}}, PrivacyRules: []PrivacyRule{{AllowedRoles: []string{"reader"}, Conditions: map[string]string{"a": "b"}}}}
	require.NoError(t, b.RegisterVerbSchema(t.Context(), verb))
	require.NoError(t, b.RegisterFactSchema(t.Context(), fact))
	require.True(t, verb.UpdatedAt.IsZero())
	require.True(t, fact.UpdatedAt.IsZero())
	defaultValue["values"].([]any)[1].([]string)[0] = "mutated"
	defaultValue["bytes"].([]byte)[0] = 9
	verb.RequiredCapabilities[0] = "mutated"
	verb.OutputSchema["sent"] = "string"
	fact.Schema["id"] = "integer"
	fact.Indexes[0].Fields[0] = "mutated"
	fact.Indexes[0].Options["a"] = "mutated"
	fact.RetentionPolicy.Conditions["a"] = "mutated"
	fact.PrivacyRules[0].AllowedRoles[0] = "mutated"
	fact.PrivacyRules[0].Conditions["a"] = "mutated"
	for i := 0; i < 3; i++ {
		v, ok := b.GetVerbSchema("notify")
		require.True(t, ok)
		d := v.InputSchema["message"].(map[string]any)["default"].(map[string]any)
		require.Equal(t, int64(9007199254740993), d["values"].([]any)[0])
		require.Equal(t, "original", d["values"].([]any)[1].([]string)[0])
		require.Equal(t, []byte{1, 2}, d["bytes"])
		require.Equal(t, []string{"send"}, v.RequiredCapabilities)
		require.Equal(t, "boolean", v.OutputSchema["sent"])
		v.RequiredCapabilities[0] = "bad"
		d["bytes"].([]byte)[0] = 9
		v.InputSchema["message"] = "bad"
		f, ok := b.GetFactSchema("order")
		require.True(t, ok)
		require.Equal(t, "string", f.Schema["id"])
		require.Equal(t, "id", f.Indexes[0].Fields[0])
		require.Equal(t, "b", f.Indexes[0].Options["a"])
		require.Equal(t, "b", f.RetentionPolicy.Conditions["a"])
		require.Equal(t, "reader", f.PrivacyRules[0].AllowedRoles[0])
		require.Equal(t, "b", f.PrivacyRules[0].Conditions["a"])
		f.Schema["id"] = "bad"
		f.Indexes[0].Fields[0] = "bad"
		f.RetentionPolicy.Conditions["a"] = "bad"
		b.ListVerbSchemas()["notify"].RequiredCapabilities[0] = "bad"
		listed := b.ListFactSchemas()["order"]
		listed.Indexes[0].Options["a"] = "bad"
		listed.PrivacyRules[0].Conditions["a"] = "bad"
		listed.PrivacyRules[0].AllowedRoles[0] = "bad"
	}
}

func TestBufRejectsUnownedValuesAndNilReaders(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	b, err := NewBufIntegration(root)
	require.NoError(t, err)
	cycle := map[string]any{}
	cycle["self"] = cycle
	for _, value := range []any{cycle, func() {}, math.NaN(), make(chan string), &struct{}{}, map[int]string{1: "bad"}} {
		s := &VerbSchema{Name: "notify", InputSchema: map[string]any{"message": map[string]any{"type": "string", "default": value}}}
		require.Error(t, b.RegisterVerbSchema(t.Context(), s))
	}
	for _, kind := range []any{"object", "array", nil, 42, map[string]any{"type": "object"}} {
		require.Error(t, b.RegisterFactSchema(t.Context(), &FactSchema{Name: "order", Schema: map[string]any{"field": kind}}))
	}
	_, err = os.Stat(root)
	require.ErrorIs(t, err, os.ErrNotExist)
	for _, zero := range []*BufIntegration{nil, {}} {
		v, ok := zero.GetVerbSchema("missing")
		require.Nil(t, v)
		require.False(t, ok)
		f, ok := zero.GetFactSchema("missing")
		require.Nil(t, f)
		require.False(t, ok)
		require.Empty(t, zero.ListVerbSchemas())
		require.Empty(t, zero.ListFactSchemas())
	}
}
