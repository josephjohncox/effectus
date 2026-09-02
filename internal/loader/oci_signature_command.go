package loader

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CommandOCISignatureVerifier delegates trust verification to a fixed
// executable. No shell is used. The executable receives reference and digest.
type CommandOCISignatureVerifier struct{ Path string }

func (verifier CommandOCISignatureVerifier) Verify(ctx context.Context, reference, digest string) error {
	path := strings.TrimSpace(verifier.Path)
	if path == "" {
		return fmt.Errorf("OCI signature verifier command is required")
	}
	command := exec.CommandContext(ctx, path, reference, digest)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("signature verifier failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
