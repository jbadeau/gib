package gib

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// entries are a tarball's file names in order, and their contents.
func entries(t *testing.T, file string) ([]string, map[string][]byte) {
	t.Helper()
	f, err := os.Open(file)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	var names []string
	data := map[string][]byte{}
	r := tar.NewReader(f)
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			return names, data
		}
		require.NoError(t, err)
		b, err := io.ReadAll(r)
		require.NoError(t, err)
		names = append(names, h.Name)
		data[h.Name] = b
	}
}

func built(t *testing.T, format ImageFormat) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "hello.txt")
	require.NoError(t, os.WriteFile(src, []byte("hello"), 0o644))
	l, err := NewFileEntriesLayerBuilder().SetName("app").AddEntry(src, "/app/hello.txt").Build()
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "image.tar")
	_, err = FromScratch().AddFileEntriesLayer(l).SetFormat(format).
		Containerize(context.Background(), ToTar(file, WithTarImageName("acme/app:1")))
	require.NoError(t, err)
	return file
}

// A Docker-format image is written as Jib's ImageTarball writes one:
// each layer as <hex>.tar.gz, then config.json, then manifest.json.
func TestImageTar_DockerFormatIsJibsLayout(t *testing.T) {
	names, data := entries(t, built(t, DockerFormat))

	require.Len(t, names, 3)
	assert.True(t, strings.HasSuffix(names[0], ".tar.gz") && !strings.Contains(names[0], "/"), names[0])
	assert.Equal(t, []string{"config.json", "manifest.json"}, names[1:])
	var m []dockerImage
	require.NoError(t, json.Unmarshal(data["manifest.json"], &m))
	assert.Equal(t, []dockerImage{{Config: "config.json", RepoTags: []string{"acme/app:1"}, Layers: names[:1]}}, m)
}

// An OCI-format image is written as Jib's ImageTarball writes one: an
// OCI image layout, every blob under blobs/sha256/, and no
// manifest.json.
func TestImageTar_OCIFormatIsJibsLayout(t *testing.T) {
	names, data := entries(t, built(t, OCIFormat))

	require.Len(t, names, 5)
	for _, n := range names[:3] {
		assert.True(t, strings.HasPrefix(n, "blobs/sha256/"), n)
	}
	assert.Equal(t, []string{"oci-layout", "index.json"}, names[3:])
	assert.JSONEq(t, `{"imageLayoutVersion": "1.0.0"}`, string(data["oci-layout"]))
	var idx v1.IndexManifest
	require.NoError(t, json.Unmarshal(data["index.json"], &idx))
	require.Len(t, idx.Manifests, 1)
	assert.Equal(t, "acme/app:1", idx.Manifests[0].Annotations["org.opencontainers.image.ref.name"])
	assert.Contains(t, names, "blobs/sha256/"+idx.Manifests[0].Digest.Hex)
}

// An OCI-format tarball, which has no manifest.json, is a base as a
// docker-save archive is.
func TestTarSource_ReadsAnOCIImageLayout(t *testing.T) {
	base := built(t, OCIFormat)
	file := filepath.Join(t.TempDir(), "image.tar")

	_, err := FromImage(TarSource(base)).SetEntrypoint("/app/hello.txt").
		Containerize(context.Background(), ToTar(file, WithTarImageName("acme/app:2")))
	require.NoError(t, err)

	_, data := entries(t, file)
	var m []dockerImage
	require.NoError(t, json.Unmarshal(data["manifest.json"], &m))
	assert.Len(t, m[0].Layers, 1, "the base's layer")
}
