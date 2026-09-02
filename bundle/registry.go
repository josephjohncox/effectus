package bundle

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// OCIVerifier verifies the pulled digest before the bundle layer is decoded.
type OCIVerifier func(context.Context, string, string) error

// PublishOCI publishes a source bundle and returns its digest-pinned immutable
// reference. imageReference can be a tag only as a publication target; callers
// must persist and deploy the returned digest reference.
func (bundle *SourceBundle) PublishOCI(ctx context.Context, imageReference string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("publish source bundle: context is nil")
	}
	reference, err := name.ParseReference(imageReference)
	if err != nil {
		return "", fmt.Errorf("parse source-bundle OCI reference: %w", err)
	}
	layerBytes, err := bundle.OCIBytes()
	if err != nil {
		return "", err
	}
	layer, err := tarball.LayerFromReader(bytes.NewReader(layerBytes))
	if err != nil {
		return "", fmt.Errorf("create source-bundle OCI layer: %w", err)
	}
	image, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		return "", fmt.Errorf("append source-bundle OCI layer: %w", err)
	}
	config, err := image.ConfigFile()
	if err != nil {
		return "", fmt.Errorf("read source-bundle OCI config: %w", err)
	}
	config.Config.Labels = map[string]string{
		"org.effectus.bundle.format":  FormatVersion,
		"org.effectus.bundle.name":    bundle.Name(),
		"org.effectus.bundle.version": bundle.Version(),
	}
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		return "", fmt.Errorf("set source-bundle OCI config: %w", err)
	}
	digest, err := image.Digest()
	if err != nil {
		return "", fmt.Errorf("digest source-bundle OCI image: %w", err)
	}
	options := []remote.Option{remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain)}
	if err := remote.Write(reference, image, options...); err != nil {
		return "", fmt.Errorf("publish source-bundle OCI image: %w", err)
	}
	published, err := remote.Head(reference, options...)
	if err != nil {
		return "", fmt.Errorf("verify published source-bundle OCI image: %w", err)
	}
	if published.Digest.String() != digest.String() {
		return "", fmt.Errorf("published source-bundle OCI digest mismatch: expected %s, got %s", digest, published.Digest)
	}
	return reference.Context().Name() + "@" + digest.String(), nil
}

// PushOCI is retained for source compatibility. New callers should use
// PublishOCI and deploy only its returned digest-pinned reference.
func (bundle *SourceBundle) PushOCI(ctx context.Context, imageReference string) error {
	_, err := bundle.PublishOCI(ctx, imageReference)
	return err
}

// PullOCI loads one digest-pinned source-bundle layer. The verifier runs after
// digest validation and before layer decoding.
func PullOCI(ctx context.Context, imageReference string, verifier OCIVerifier) (*SourceBundle, error) {
	if ctx == nil {
		return nil, fmt.Errorf("pull source bundle: context is nil")
	}
	reference, err := name.ParseReference(imageReference)
	if err != nil {
		return nil, fmt.Errorf("parse source-bundle OCI reference: %w", err)
	}
	if _, pinned := reference.(name.Digest); !pinned {
		return nil, fmt.Errorf("source-bundle OCI reference must be pinned by digest")
	}
	image, err := remote.Image(reference, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return nil, fmt.Errorf("pull source-bundle OCI image: %w", err)
	}
	digest, err := image.Digest()
	if err != nil {
		return nil, fmt.Errorf("read source-bundle OCI digest: %w", err)
	}
	if digest.String() != reference.Identifier() {
		return nil, fmt.Errorf("source-bundle OCI digest mismatch: expected %s, got %s", reference.Identifier(), digest.String())
	}
	if verifier == nil {
		return nil, fmt.Errorf("source-bundle OCI verifier is required")
	}
	if err := verifier(ctx, reference.Context().Name(), digest.String()); err != nil {
		return nil, fmt.Errorf("verify source-bundle OCI image: %w", err)
	}
	layers, err := image.Layers()
	if err != nil {
		return nil, fmt.Errorf("read source-bundle OCI layers: %w", err)
	}
	if len(layers) != 1 {
		return nil, fmt.Errorf("source-bundle OCI image must contain exactly one layer")
	}
	reader, err := layers[0].Uncompressed()
	if err != nil {
		return nil, fmt.Errorf("open source-bundle OCI layer: %w", err)
	}
	defer reader.Close()
	const maxSourceBundleLayerBytes = 64 << 20
	data, err := io.ReadAll(io.LimitReader(reader, maxSourceBundleLayerBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read source-bundle OCI layer: %w", err)
	}
	if len(data) > maxSourceBundleLayerBytes {
		return nil, fmt.Errorf("source-bundle OCI layer exceeds %d bytes", maxSourceBundleLayerBytes)
	}
	return ParseOCI(data)
}
