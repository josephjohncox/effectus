// unified/oci.go
package unified

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"archive/tar"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/josephjohncox/effectus/internal/loader"
	"github.com/josephjohncox/effectus/internal/safetar"
)

// OCIBundlePusher handles pushing bundles to OCI registries
type OCIBundlePusher struct {
	bundle    *Bundle
	schemaDir string
	verbDir   string
	rulesDir  string
}

// NewOCIBundlePusher creates a new OCI bundle pusher
func NewOCIBundlePusher(bundle *Bundle) *OCIBundlePusher {
	return &OCIBundlePusher{
		bundle: bundle,
	}
}

// WithSchemaDir sets the schema directory
func (p *OCIBundlePusher) WithSchemaDir(dir string) *OCIBundlePusher {
	p.schemaDir = dir
	return p
}

// WithVerbDir sets the verb directory
func (p *OCIBundlePusher) WithVerbDir(dir string) *OCIBundlePusher {
	p.verbDir = dir
	return p
}

// WithRulesDir sets the rules directory
func (p *OCIBundlePusher) WithRulesDir(dir string) *OCIBundlePusher {
	p.rulesDir = dir
	return p
}

// Push pushes the bundle to an OCI registry
func (p *OCIBundlePusher) Push(imageRef string) error {
	// Parse the reference
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return fmt.Errorf("parsing reference: %w", err)
	}

	// Start with an empty image
	img := empty.Image

	// Add layers
	layers := []struct {
		name  string
		dir   string
		files []string
	}{
		{"schema", p.schemaDir, p.bundle.SchemaFiles},
		{"verbs", p.verbDir, p.bundle.VerbFiles},
		{"rules", p.rulesDir, p.bundle.RuleFiles},
	}

	// Create and append each layer
	for _, layer := range layers {
		if layer.dir == "" || len(layer.files) == 0 {
			continue
		}

		layerBytes, err := p.createLayerTar(layer.dir, layer.files)
		if err != nil {
			return fmt.Errorf("creating %s layer: %w", layer.name, err)
		}

		layerImage, err := tarball.LayerFromReader(bytes.NewReader(layerBytes))
		if err != nil {
			return fmt.Errorf("creating %s layer image: %w", layer.name, err)
		}

		img, err = mutate.AppendLayers(img, layerImage)
		if err != nil {
			return fmt.Errorf("appending %s layer: %w", layer.name, err)
		}
	}

	// Add bundle metadata as the final layer
	bundleJSON, err := json.Marshal(p.bundle)
	if err != nil {
		return fmt.Errorf("marshaling bundle: %w", err)
	}

	bundleLayer, err := tarball.LayerFromReader(bytes.NewReader(bundleJSON))
	if err != nil {
		return fmt.Errorf("creating bundle layer: %w", err)
	}

	img, err = mutate.AppendLayers(img, bundleLayer)
	if err != nil {
		return fmt.Errorf("appending bundle layer: %w", err)
	}

	// Add bundle info to image config
	configFile, err := img.ConfigFile()
	if err != nil {
		return fmt.Errorf("getting config file: %w", err)
	}

	configFile.Config.Labels = map[string]string{
		"org.effectus.bundle.name":     p.bundle.Name,
		"org.effectus.bundle.version":  p.bundle.Version,
		"org.effectus.bundle.verbHash": p.bundle.VerbHash,
	}

	img, err = mutate.Config(img, configFile.Config)
	if err != nil {
		return fmt.Errorf("updating image config: %w", err)
	}

	// Push the image
	if err := remote.Write(ref, img, remote.WithAuthFromKeychain(authn.DefaultKeychain)); err != nil {
		return fmt.Errorf("pushing image: %w", err)
	}

	return nil
}

// createLayerTar creates a tar archive containing the specified files
func (p *OCIBundlePusher) createLayerTar(dir string, files []string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	for _, file := range files {
		// Read the file
		fullPath := filepath.Join(dir, file)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("reading file %s: %w", file, err)
		}

		// Add to tar
		if err := tw.WriteHeader(&tar.Header{
			Name: file,
			Size: int64(len(data)),
			Mode: 0644,
		}); err != nil {
			return nil, fmt.Errorf("writing header for %s: %w", file, err)
		}

		if _, err := tw.Write(data); err != nil {
			return nil, fmt.Errorf("writing content for %s: %w", file, err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("closing tar writer: %w", err)
	}

	return buf.Bytes(), nil
}

// OCIBundlePuller handles pulling bundles from OCI registries
type OCIBundlePuller struct {
	outputDir    string
	verification loader.OCIVerificationPolicy
}

// NewOCIBundlePuller creates a new OCI bundle puller
func NewOCIBundlePuller(outputDir string) *OCIBundlePuller {
	return NewOCIBundlePullerWithPolicy(outputDir, loader.OCIVerificationPolicy{})
}
func NewOCIBundlePullerWithPolicy(outputDir string, verification loader.OCIVerificationPolicy) *OCIBundlePuller {
	return &OCIBundlePuller{outputDir: outputDir, verification: verification}
}

// Pull pulls a bundle from an OCI registry
func (p *OCIBundlePuller) Pull(imageRef string) (*Bundle, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	bundle, _, err := p.PullWithDataContext(ctx, imageRef)
	return bundle, err
}

// PullWithData pulls a bundle and returns the raw bundle metadata bytes.
func (p *OCIBundlePuller) PullWithData(imageRef string) (*Bundle, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return p.PullWithDataContext(ctx, imageRef)
}

func (p *OCIBundlePuller) PullWithDataContext(ctx context.Context, imageRef string) (*Bundle, []byte, error) {
	// Parse the reference
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing reference: %w", err)
	}
	if _, pinned := ref.(name.Digest); !pinned {
		return nil, nil, fmt.Errorf("OCI bundle reference must be pinned by digest")
	}

	// Pull the image
	img, err := remote.Image(ref, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return nil, nil, fmt.Errorf("pulling image: %w", err)
	}
	actualDigest, err := img.Digest()
	if err != nil {
		return nil, nil, fmt.Errorf("read OCI bundle digest: %w", err)
	}
	if actualDigest.String() != ref.Identifier() {
		return nil, nil, fmt.Errorf("OCI bundle digest mismatch: expected %s, got %s", ref.Identifier(), actualDigest.String())
	}
	if p.verification.RequireSignature && p.verification.Verifier == nil {
		return nil, nil, fmt.Errorf("OCI bundle signature verification is required but no verifier is configured")
	}
	if p.verification.Verifier != nil {
		if err := p.verification.Verifier.Verify(ctx, ref.Name(), actualDigest.String()); err != nil {
			return nil, nil, fmt.Errorf("verify OCI bundle signature: %w", err)
		}
	}

	// Get the layers
	layers, err := img.Layers()
	if err != nil {
		return nil, nil, fmt.Errorf("getting layers: %w", err)
	}

	// Ensure the image has layers
	if len(layers) == 0 {
		return nil, nil, fmt.Errorf("image has no layers")
	}

	// Extract the bundle metadata from the last layer
	bundleLayer := layers[len(layers)-1]
	bundleContent, err := bundleLayer.Uncompressed()
	if err != nil {
		return nil, nil, fmt.Errorf("getting bundle layer: %w", err)
	}
	defer bundleContent.Close()

	const maxBundleMetadataBytes = 4 << 20
	bundleData, err := io.ReadAll(io.LimitReader(bundleContent, maxBundleMetadataBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("reading bundle data: %w", err)
	}
	if len(bundleData) > maxBundleMetadataBytes {
		return nil, nil, fmt.Errorf("OCI bundle metadata exceeds %d bytes", maxBundleMetadataBytes)
	}

	var bundle Bundle
	if err := json.Unmarshal(bundleData, &bundle); err != nil {
		return nil, nil, fmt.Errorf("unmarshaling bundle: %w", err)
	}

	// Extract content layers if outputDir is specified
	if p.outputDir != "" {
		// Create output directories
		dirs := []string{"schema", "verbs", "rules"}
		for _, dir := range dirs {
			dirPath := filepath.Join(p.outputDir, dir)
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				return nil, nil, fmt.Errorf("creating directory %s: %w", dir, err)
			}
		}

		// Extract each layer to its corresponding directory
		for i, layer := range layers[:len(layers)-1] {
			// Skip if index is out of bounds
			if i >= len(dirs) {
				continue
			}

			// Get the uncompressed layer content
			rc, err := layer.Uncompressed()
			if err != nil {
				return nil, nil, fmt.Errorf("getting layer %d: %w", i, err)
			}

			// Extract the layer contents manually
			targetDir := filepath.Join(p.outputDir, dirs[i])
			if err := extractTarLayer(rc, targetDir); err != nil {
				rc.Close()
				return nil, nil, fmt.Errorf("extracting layer %d: %w", i, err)
			}
			rc.Close()
		}

		// Save bundle metadata as JSON file
		bundleFile := filepath.Join(p.outputDir, "bundle.json")
		if err := os.WriteFile(bundleFile, bundleData, 0644); err != nil {
			return nil, nil, fmt.Errorf("writing bundle file: %w", err)
		}
	}

	return &bundle, bundleData, nil
}

// extractTarLayer extracts a bounded tar stream beneath targetDir.
func extractTarLayer(r io.Reader, targetDir string) error {
	if err := safetar.Extract(r, targetDir, safetar.DefaultLimits()); err != nil {
		return fmt.Errorf("extracting safe tar layer: %w", err)
	}
	return nil
}
