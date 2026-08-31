package verb

import (
	"testing"

	"github.com/josephjohncox/effectus/schema/types"
	"github.com/stretchr/testify/require"
)

func TestRuntimeCapabilityUsesStrongestAccessFlag(t *testing.T) {
	tests := []struct {
		name string
		cap  Capability
		want types.Capability
	}{
		{name: "none defaults safe", cap: CapNone, want: types.CapabilityModify},
		{name: "read", cap: CapRead, want: types.CapabilityRead},
		{name: "write", cap: CapWrite, want: types.CapabilityModify},
		{name: "read write", cap: CapReadWrite, want: types.CapabilityModify},
		{name: "create", cap: CapRead | CapCreate, want: types.CapabilityCreate},
		{name: "delete", cap: CapAll, want: types.CapabilityDelete},
		{name: "semantic flags do not weaken", cap: CapRead | CapWrite | CapIdempotent, want: types.CapabilityModify},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, test.cap.RuntimeCapability())
		})
	}
}
