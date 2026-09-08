package schema

import (
	"context"
	"fmt"
)

func (b *BufIntegration) checkContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if b == nil || b.workspaceRoot == "" || b.verbRegistry == nil || b.factRegistry == nil {
		return fmt.Errorf("BufIntegration is not initialized. Use NewBufIntegration")
	}
	return nil
}
