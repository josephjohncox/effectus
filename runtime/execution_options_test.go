package runtime

import (
	"testing"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestGeneratedExecutionOptionDeprecations(t *testing.T) {
	fields := (&effectusv1.ExecutionOptions{}).ProtoReflect().Descriptor().Fields()
	for _, name := range []string{"dry_run", "max_effects", "enable_tracing", "capability_filter", "min_schema_version", "max_schema_version"} {
		field := fields.ByName(protoreflect.Name(name))
		require.NotNil(t, field)
		options, ok := field.Options().(*descriptorpb.FieldOptions)
		require.True(t, ok)
		require.True(t, options.GetDeprecated(), name)
	}
	options := fields.ByName("timeout_seconds").Options().(*descriptorpb.FieldOptions)
	require.False(t, options.GetDeprecated())
}

func TestValidateExecutionOptions(t *testing.T) {
	tests := []struct {
		name  string
		value *effectusv1.ExecutionOptions
		field string
	}{
		{"dry run", &effectusv1.ExecutionOptions{DryRun: true}, "dry_run"},
		{"max effects", &effectusv1.ExecutionOptions{MaxEffects: 1}, "max_effects"},
		{"tracing", &effectusv1.ExecutionOptions{EnableTracing: true}, "enable_tracing"},
		{"capabilities", &effectusv1.ExecutionOptions{CapabilityFilter: []string{"write"}}, "capability_filter"},
		{"min schema", &effectusv1.ExecutionOptions{MinSchemaVersion: "1"}, "min_schema_version"},
		{"max schema", &effectusv1.ExecutionOptions{MaxSchemaVersion: "2"}, "max_schema_version"},
		{"negative timeout", &effectusv1.ExecutionOptions{TimeoutSeconds: -1}, "timeout_seconds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateExecutionOptions(test.value)
			require.ErrorContains(t, err, test.field)
		})
	}
	require.NoError(t, validateExecutionOptions(nil))
	require.NoError(t, validateExecutionOptions(&effectusv1.ExecutionOptions{TimeoutSeconds: 5}))
}
