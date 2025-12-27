package schemasources

import (
	"context"
	"testing"

	"github.com/effectus/effectus-go/adapters"
	"github.com/effectus/effectus-go/schema/types"
)

type testSchemaFactory struct{}

type testSchemaProvider struct {
	defs []adapters.SchemaDefinition
}

func (f *testSchemaFactory) ValidateConfig(config adapters.SchemaSourceConfig) error {
	return nil
}

func (f *testSchemaFactory) Create(config adapters.SchemaSourceConfig) (adapters.SchemaProvider, error) {
	name, _ := config.Config["name"].(string)
	schema, _ := config.Config["schema"].(string)
	return &testSchemaProvider{defs: []adapters.SchemaDefinition{{
		Name:   name,
		Format: adapters.SchemaFormatJSONSchema,
		Data:   []byte(schema),
	}}}, nil
}

func (f *testSchemaFactory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{}
}

func (p *testSchemaProvider) LoadSchemas(ctx context.Context) ([]adapters.SchemaDefinition, error) {
	return p.defs, nil
}

func (p *testSchemaProvider) Close() error {
	return nil
}

func TestSchemaSourcesReload(t *testing.T) {
	providerType := "test_schema_reload"
	_ = adapters.RegisterSchemaProvider(providerType, &testSchemaFactory{})

	ctx := context.Background()
	typeSystem := types.NewTypeSystem()

	first := adapters.SchemaSourceConfig{
		Type:      providerType,
		Namespace: "demo",
		Config: map[string]interface{}{
			"name":   "one",
			"schema": `{"type":"object","properties":{"value":{"type":"string"}}}`,
		},
	}

	if err := Apply(ctx, typeSystem, []adapters.SchemaSourceConfig{first}, false); err != nil {
		t.Fatalf("apply first schema: %v", err)
	}
	if _, err := typeSystem.GetFactType("demo.one.value"); err != nil {
		t.Fatalf("expected demo.one.value to exist: %v", err)
	}

	typeSystem.ResetFactTypes()

	second := adapters.SchemaSourceConfig{
		Type:      providerType,
		Namespace: "demo",
		Config: map[string]interface{}{
			"name":   "two",
			"schema": `{"type":"object","properties":{"other":{"type":"boolean"}}}`,
		},
	}

	if err := Apply(ctx, typeSystem, []adapters.SchemaSourceConfig{second}, false); err != nil {
		t.Fatalf("apply second schema: %v", err)
	}
	if _, err := typeSystem.GetFactType("demo.one.value"); err == nil {
		t.Fatalf("expected demo.one.value to be removed on reload")
	}
	if _, err := typeSystem.GetFactType("demo.two.other"); err != nil {
		t.Fatalf("expected demo.two.other to exist: %v", err)
	}
}
