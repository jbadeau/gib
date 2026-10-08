package gib_test

import (
	"archive/tar"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/jbadeau/gib"
	"github.com/jbadeau/gib/buildfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A build file's platforms leave a tarball base of one image alone, as
// Jib's do: the image is built on it as it is.
func TestABuildFilesPlatformsLeaveASingleImageTarballAlone(t *testing.T) {
	dir := t.TempDir()
	img, err := random.Image(64, 1)
	require.NoError(t, err)
	cfg, err := img.ConfigFile()
	require.NoError(t, err)
	cfg = cfg.DeepCopy()
	cfg.OS, cfg.Architecture = "linux", "amd64"
	img, err = mutate.ConfigFile(img, cfg)
	require.NoError(t, err)
	tag, err := name.NewTag("base:1")
	require.NoError(t, err)
	base := filepath.Join(dir, "base.tar")
	require.NoError(t, tarball.WriteToFile(base, tag, img))

	got := buildOn(t, dir, base, "arm64")

	assert.Equal(t, "linux/amd64", got)
}

// A build file's platforms choose from a tarball base of several, an
// OCI image layout with an index as apko writes one: each is built on
// the index's image for it.
func TestABuildFilesPlatformChoosesFromAMultiArchTarball(t *testing.T) {
	dir := t.TempDir()
	base := multiArch(t, dir, "amd64", "arm64")

	for _, arch := range []string{"amd64", "arm64"} {
		assert.Equal(t, "linux/"+arch, buildOn(t, dir, base, arch))
	}
}

// multiArch writes an OCI image layout of one image per architecture,
// tarred.
func multiArch(t *testing.T, dir string, archs ...string) string {
	t.Helper()
	idx := mutate.IndexMediaType(empty.Index, types.OCIImageIndex)
	for _, a := range archs {
		img, err := random.Image(64, 1)
		require.NoError(t, err)
		cfg, err := img.ConfigFile()
		require.NoError(t, err)
		cfg = cfg.DeepCopy()
		cfg.OS, cfg.Architecture = "linux", a
		img, err = mutate.ConfigFile(img, cfg)
		require.NoError(t, err)
		idx = mutate.AppendManifests(idx, mutate.IndexAddendum{Add: img, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: a}}})
	}
	lay := filepath.Join(dir, "layout")
	p, err := layout.Write(lay, empty.Index)
	require.NoError(t, err)
	m, err := idx.IndexManifest()
	require.NoError(t, err)
	for _, d := range m.Manifests {
		img, err := idx.Image(d.Digest)
		require.NoError(t, err)
		require.NoError(t, p.AppendImage(img, layout.WithPlatform(*d.Platform)))
	}
	out := filepath.Join(dir, "base.tar")
	f, err := os.Create(out)
	require.NoError(t, err)
	tw := tar.NewWriter(f)
	require.NoError(t, filepath.Walk(lay, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		rel, err := filepath.Rel(lay, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel), Mode: 0o644, Size: int64(len(b))}); err != nil {
			return err
		}
		_, err = tw.Write(b)
		return err
	}))
	require.NoError(t, tw.Close())
	require.NoError(t, f.Close())
	return out
}

func buildOn(t *testing.T, dir, base, arch string) string {
	t.Helper()
	file := filepath.Join(dir, "jib.yaml")
	require.NoError(t, os.WriteFile(file, []byte("apiVersion: jib/v1alpha1\nkind: BuildFile\nfrom:\n  image: tar://"+base+
		"\n  platforms: [{architecture: "+arch+", os: linux}]\n"), 0o644))
	spec, err := buildfile.Parse(file, nil)
	require.NoError(t, err)
	b, err := buildfile.Convert(spec, dir, nil)
	require.NoError(t, err)
	out := filepath.Join(t.TempDir(), "image.tar")
	_, err = b.Containerize(context.Background(), gib.ToTar(out, gib.WithTarImageName("app")))
	require.NoError(t, err)
	built, err := tarball.ImageFromPath(out, nil)
	require.NoError(t, err)
	c, err := built.ConfigFile()
	require.NoError(t, err)
	return c.OS + "/" + c.Architecture
}
