package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/jbadeau/gib/internal/layer"
)

func TestExecute_FromScratch_WithLayers(t *testing.T) {
	dir := testdataDir(t)

	entries := []layer.Entry{
		{SourcePath: filepath.Join(dir, "foo"), DestinationPath: "/app/foo", Permissions: 0644, ModificationTime: time.UnixMilli(1000), Ownership: "0:0"},
	}

	l, err := layer.BuildReproducibleLayer(entries)
	require.NoError(t, err)

	req := Request{
		BaseImage:        empty.Image,
		Layers:           []v1.Layer{l},
		Entrypoint:       []string{"/app/foo"},
		User:             "appuser",
		WorkingDirectory: "/app",
		Environment:      map[string]string{"KEY": "VAL"},
		Labels:           map[string]string{"label": "value"},
		ExposedPorts:     []string{"8080/tcp"},
		Volumes:          []string{"/data"},
		MediaType:        types.DockerManifestSchema2,
	}

	image, err := Execute(context.Background(), req)
	require.NoError(t, err)

	// Verify config
	cfg, err := image.ConfigFile()
	require.NoError(t, err)

	assert.Equal(t, []string{"/app/foo"}, cfg.Config.Entrypoint)
	assert.Equal(t, "appuser", cfg.Config.User)
	assert.Equal(t, "/app", cfg.Config.WorkingDir)
	assert.Contains(t, cfg.Config.Env, "KEY=VAL")
	assert.Equal(t, "value", cfg.Config.Labels["label"])
	assert.Contains(t, cfg.Config.ExposedPorts, "8080/tcp")
	assert.Contains(t, cfg.Config.Volumes, "/data")

	// Verify layers
	layers, err := image.Layers()
	require.NoError(t, err)
	assert.Len(t, layers, 1)
}

func TestExecute_TarRoundtrip(t *testing.T) {
	dir := testdataDir(t)

	entries := []layer.Entry{
		{SourcePath: filepath.Join(dir, "foo"), DestinationPath: "/app/foo", Permissions: 0644, ModificationTime: time.UnixMilli(1000), Ownership: "0:0"},
		{SourcePath: filepath.Join(dir, "a/b/bar"), DestinationPath: "/app/bar", Permissions: 0755, ModificationTime: time.UnixMilli(1000), Ownership: "0:0"},
	}

	l, err := layer.BuildReproducibleLayer(entries)
	require.NoError(t, err)

	req := Request{
		BaseImage:  empty.Image,
		Layers:     []v1.Layer{l},
		Entrypoint: []string{"/app/foo"},
		MediaType:  types.DockerManifestSchema2,
	}

	image, err := Execute(context.Background(), req)
	require.NoError(t, err)

	// Write to tar
	tmpFile := filepath.Join(t.TempDir(), "image.tar")
	tag, err := name.NewTag("gib/test:latest")
	require.NoError(t, err)
	err = tarball.WriteToFile(tmpFile, tag, image)
	require.NoError(t, err)

	// Read back
	readImage, err := tarball.ImageFromPath(tmpFile, &tag)
	require.NoError(t, err)

	// Verify digest matches
	origDigest, _ := image.Digest()
	readDigest, _ := readImage.Digest()
	assert.Equal(t, origDigest, readDigest)

	// Verify config
	cfg, err := readImage.ConfigFile()
	require.NoError(t, err)
	assert.Equal(t, []string{"/app/foo"}, cfg.Config.Entrypoint)

	// Verify layer contents
	layers, err := readImage.Layers()
	require.NoError(t, err)
	require.Len(t, layers, 1)

	// Read layer and check files
	rc, err := layers[0].Compressed()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()

	var buf bytes.Buffer
	_, err = io.Copy(&buf, rc)
	require.NoError(t, err)

	gr, err := gzip.NewReader(&buf)
	require.NoError(t, err)
	tr := tar.NewReader(gr)

	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if hdr.Typeflag == tar.TypeReg {
			names = append(names, hdr.Name)
		}
	}
	assert.Contains(t, names, "app/bar")
	assert.Contains(t, names, "app/foo")
}

func TestExecute_CreationTime(t *testing.T) {
	created := time.Date(2020, 1, 1, 0, 0, 0, 123456789, time.UTC)
	req := Request{
		BaseImage:    empty.Image,
		CreationTime: &created,
		MediaType:    types.DockerManifestSchema2,
	}

	image, err := Execute(context.Background(), req)
	require.NoError(t, err)

	cfg, err := image.ConfigFile()
	require.NoError(t, err)

	assert.Equal(t, created, cfg.Created.Time, "nanoseconds kept, as Instant keeps them")
}

func TestExecute_OCIFormat(t *testing.T) {
	req := Request{
		BaseImage: empty.Image,
		MediaType: types.OCIManifestSchema1,
	}

	image, err := Execute(context.Background(), req)
	require.NoError(t, err)

	mt, err := image.MediaType()
	require.NoError(t, err)
	assert.Equal(t, types.OCIManifestSchema1, mt)
}

func TestExecute_Platform(t *testing.T) {
	arm64 := v1.Platform{OS: "linux", Architecture: "arm64"}
	amd64Base, err := mutate.ConfigFile(empty.Image, &v1.ConfigFile{OS: "linux", Architecture: "amd64"})
	require.NoError(t, err)

	tests := []struct {
		name     string
		base     v1.Image
		platform v1.Platform
		want     v1.Platform
	}{
		{name: "scratch takes the platform", base: empty.Image, platform: arm64, want: arm64},
		{name: "a base keeps its own", base: amd64Base, platform: v1.Platform{OS: "linux", Architecture: "amd64"}, want: v1.Platform{OS: "linux", Architecture: "amd64"}},
		{name: "a base of another platform keeps its own", base: amd64Base, platform: arm64, want: v1.Platform{OS: "linux", Architecture: "amd64"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			image, err := Execute(context.Background(), Request{BaseImage: tt.base, Platform: tt.platform})
			require.NoError(t, err)
			cfg, err := image.ConfigFile()
			require.NoError(t, err)
			assert.Equal(t, tt.want, v1.Platform{OS: cfg.OS, Architecture: cfg.Architecture})
		})
	}
}

func testdataDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "layers", "simple"))
	require.NoError(t, err)
	_, err = os.Stat(dir)
	require.NoError(t, err)
	return dir
}

func TestExecute_TheSameBuildIsTheSameImage(t *testing.T) {
	req := Request{BaseImage: empty.Image, Environment: map[string]string{"D": "4", "A": "1", "C": "3", "B": "2", "E": "5"}}
	first, err := Execute(context.Background(), req)
	require.NoError(t, err)
	want, err := first.Digest()
	require.NoError(t, err)
	for range 20 {
		img, err := Execute(context.Background(), req)
		require.NoError(t, err)
		got, err := img.Digest()
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

func TestExecute_RefusesWhatJibRefuses(t *testing.T) {
	malformed, err := mutate.ConfigFile(empty.Image, &v1.ConfigFile{Config: v1.Config{Env: []string{"NOEQ"}}})
	require.NoError(t, err)
	tests := map[string]struct {
		req Request
		err string
	}{
		"an environment name with =":         {Request{BaseImage: empty.Image, Environment: map[string]string{"A=B": "v"}}, "cannot contain '='"},
		"a base environment entry without =": {Request{BaseImage: malformed}, `"NOEQ" is not NAME=VALUE`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Execute(context.Background(), tt.req)
			require.ErrorContains(t, err, tt.err)
		})
	}
}
