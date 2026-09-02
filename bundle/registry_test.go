package bundle

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/stretchr/testify/require"
)

func TestPullOCIRejectsUnverifiedImageBeforeDecodingLayer(t *testing.T) {
	server := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)

	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	require.NoError(t, writer.WriteHeader(&tar.Header{Name: "unexpected", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}))
	_, err = writer.Write([]byte("x"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	layer, err := tarball.LayerFromReader(bytes.NewReader(archive.Bytes()))
	require.NoError(t, err)
	image, err := mutate.AppendLayers(empty.Image, layer)
	require.NoError(t, err)
	tag, err := name.NewTag(endpoint.Host + "/bundles/orders:staged")
	require.NoError(t, err)
	require.NoError(t, remote.Write(tag, image))
	digest, err := image.Digest()
	require.NoError(t, err)

	_, err = PullOCI(context.Background(), endpoint.Host+"/bundles/orders@"+digest.String(), func(context.Context, string, string) error {
		return errors.New("signature rejected")
	})
	require.ErrorContains(t, err, "verify source-bundle OCI image: signature rejected")
}
