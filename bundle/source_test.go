package bundle

import (
	"testing"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/stretchr/testify/require"
)

func TestSourceBundleCanonicalAcrossInputOrderAndMetadata(t *testing.T) {
	descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{
		Type: invocation.DescriptorHTTP, ResolverID: "http/v1", Reference: "https://executor.example/review",
	})
	require.NoError(t, err)
	environment := ir.Environment{
		Facts: map[string]string{"order.total": "float"},
		Verbs: map[string]ir.VerbContract{"RequestReview": {Arguments: map[string]string{"order_id": "string"}, RequiredArgs: []string{"order_id"}, ResultType: "string"}},
	}
	first, err := New(Spec{
		Name: "orders", Version: "1", Environment: environment,
		Sources:   []Source{{Path: "rules/z.effx", Content: "flow Z {}\r\n"}, {Path: "rules/a.eff", Content: "rule A {}\r\n"}},
		Executors: map[string]invocation.Descriptor{"RequestReview": descriptor}, Metadata: map[string]string{"built_by": "one"},
	})
	require.NoError(t, err)
	second, err := New(Spec{
		Name: "orders", Version: "1", Environment: environment,
		Sources:   []Source{{Path: "rules/a.eff", Content: "rule A {}\n"}, {Path: "rules/z.effx", Content: "flow Z {}\n"}},
		Executors: map[string]invocation.Descriptor{"RequestReview": descriptor}, Metadata: map[string]string{"built_by": "two"},
	})
	require.NoError(t, err)
	firstDigest, err := first.Digest()
	require.NoError(t, err)
	secondDigest, err := second.Digest()
	require.NoError(t, err)
	require.Equal(t, firstDigest, secondDigest)
	require.Equal(t, "rules/a.eff", first.Sources()[0].Path)
}

func TestSourceBundleFileAndOCIEncodingsDecodeIdentically(t *testing.T) {
	original, err := New(Spec{Name: "orders", Version: "1", Sources: []Source{{Path: "rules/orders.eff", Content: "rule Orders {}\n"}}, Environment: ir.Environment{}})
	require.NoError(t, err)
	fileBytes, err := original.Bytes()
	require.NoError(t, err)
	fromFile, err := Parse(fileBytes)
	require.NoError(t, err)
	ociBytes, err := original.OCIBytes()
	require.NoError(t, err)
	fromOCI, err := ParseOCI(ociBytes)
	require.NoError(t, err)
	fromFileBytes, err := fromFile.Bytes()
	require.NoError(t, err)
	fromOCIBytes, err := fromOCI.Bytes()
	require.NoError(t, err)
	require.Equal(t, fromFileBytes, fromOCIBytes)
}

func TestSourceBundleRejectsAmbiguousInput(t *testing.T) {
	_, err := New(Spec{Name: "orders", Version: "1", Environment: ir.Environment{}, Sources: []Source{{Path: "../orders.eff", Content: "x"}}})
	require.ErrorContains(t, err, "normalized relative path")
	_, err = New(Spec{Name: "orders", Version: "1", Environment: ir.Environment{}, Sources: []Source{{Path: "rules/orders.eff", Content: "x"}, {Path: "rules/orders.eff", Content: "y"}}})
	require.ErrorContains(t, err, "repeats source path")
	_, err = Parse([]byte(`{"format_version":"effectus.source-bundle.v1","name":"orders","name":"duplicate","version":"1","sources":[],"environment":{}}`))
	require.ErrorContains(t, err, "duplicate")
	_, err = Parse([]byte(`{"format_version":"effectus.source-bundle.v1","name":"orders","version":"1","sources":[],"environment":{},"created_at":"now"}`))
	require.ErrorContains(t, err, "unknown field")
}
