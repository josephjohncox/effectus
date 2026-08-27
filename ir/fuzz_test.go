package ir_test

import (
	"testing"

	"github.com/effectus/effectus-go/ir"
)

func FuzzParse(f *testing.F) {
	// The malformed seeds exercise empty, truncated, and unknown-field inputs.
	f.Add([]byte(nil))
	f.Add([]byte{0x08, 0x01})
	f.Add([]byte{0xc0, 0x3e, 0x01})
	f.Fuzz(func(t *testing.T, data []byte) {
		environment := testEnvironment(t)
		checked, err := ir.Parse(data, environment, ir.Limits{MaxArtifactBytes: 64 << 10})
		if err != nil {
			return
		}
		roundTrip, err := ir.Parse(checked.Marshal(), environment, ir.Limits{MaxArtifactBytes: 64 << 10})
		if err != nil {
			t.Fatalf("accepted artifact did not reparse: %v", err)
		}
		if checked.Digest() != roundTrip.Digest() {
			t.Fatalf("non-deterministic digest: %s != %s", checked.Digest(), roundTrip.Digest())
		}
	})
}
