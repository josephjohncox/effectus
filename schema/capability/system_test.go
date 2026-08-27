package capability

import (
	"testing"
	"time"

	effectus "github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/invocation"
	"github.com/effectus/effectus-go/schema/fencing"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/stretchr/testify/require"
)

type capabilityValidatorFunc func(CapabilityContext) error

func (f capabilityValidatorFunc) Validate(ctx CapabilityContext) error {
	return f(ctx)
}

func TestStaleUnlockDoesNotDeleteReplacementLock(t *testing.T) {
	system := NewCapabilitySystem()
	system.RegisterResourcePolicy(&ResourcePolicy{ResourceType: "account-1", DefaultTimeout: 5 * time.Millisecond})

	first, err := system.AcquireLock(types.CapabilityModify, "account-1", "first")
	require.NoError(t, err)
	time.Sleep(15 * time.Millisecond)

	second, err := system.AcquireLock(types.CapabilityModify, "account-1", "second")
	require.NoError(t, err)
	require.Equal(t, fencing.GuaranteeLocalAdvisory, second.Guarantee)
	require.Equal(t, invocation.FencingLocalLockOnly, second.FencingStatus)
	require.Greater(t, second.FenceToken, first.FenceToken)

	first.Unlock()
	_, err = system.AcquireLock(types.CapabilityModify, "account-1", "third")
	require.ErrorContains(t, err, "held by second")

	second.Unlock()
	third, err := system.AcquireLock(types.CapabilityModify, "account-1", "third")
	require.NoError(t, err)
	third.Unlock()
}

func TestReadLocksRejectUntrackedOverlap(t *testing.T) {
	system := NewCapabilitySystem()
	first, err := system.AcquireLock(types.CapabilityRead, "account-1", "first")
	require.NoError(t, err)
	defer first.Unlock()

	_, err = system.AcquireLock(types.CapabilityRead, "account-1", "second")
	require.ErrorContains(t, err, "resource locked")
	_, err = system.AcquireLock(types.CapabilityModify, "account-1", "writer")
	require.ErrorContains(t, err, "resource locked")
}

func TestCapabilityValidatorReceivesLockHolder(t *testing.T) {
	system := NewCapabilitySystem()
	var holder string
	system.RegisterResourcePolicy(&ResourcePolicy{
		ResourceType:       "update-account",
		RequiredCapability: types.CapabilityModify,
		Validators: []CapabilityValidator{capabilityValidatorFunc(func(ctx CapabilityContext) error {
			holder = ctx.Holder
			return nil
		})},
	})

	err := system.ValidateCapabilityForHolder(
		effectus.Effect{Verb: "update-account", Payload: map[string]interface{}{"id": "account-1"}},
		types.CapabilityModify,
		"request-42",
	)
	require.NoError(t, err)
	require.Equal(t, "request-42", holder)
}
