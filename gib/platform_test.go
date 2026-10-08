package gib

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These are Jib's platform rules, from its PlatformChecker,
// PullBaseImageStep and StepsRunner: what Jib builds, gib builds the
// same; where Jib refuses, gib refuses with Jib's message.

func scratchFor(platforms ...string) *ContainerBuilder {
	b := FromScratch().SetEntrypoint("/app")
	for _, p := range platforms {
		b.AddPlatform(p, "linux")
	}
	return b
}

func tarOf(t *testing.T, b *ContainerBuilder) (string, error) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "image.tar")
	_, err := b.Containerize(context.Background(), ToTar(file, WithTarImageName("acme/app:1")))
	return file, err
}

func platformOf(t *testing.T, img v1.Image) string {
	t.Helper()
	cfg, err := img.ConfigFile()
	require.NoError(t, err)
	return cfg.OS + "/" + cfg.Architecture
}

func TestPlatform_ScratchIsBuiltForThePlatformNamed(t *testing.T) {
	for want, platforms := range map[string][]string{"linux/amd64": nil, "linux/arm64": {"arm64"}} {
		file, err := tarOf(t, scratchFor(platforms...))
		require.NoError(t, err)
		img, err := tarball.ImageFromPath(file, nil)
		require.NoError(t, err)
		assert.Equal(t, want, platformOf(t, img))
	}
}

func TestPlatform_ATarballTargetTakesOnePlatform(t *testing.T) {
	_, err := tarOf(t, scratchFor("amd64", "arm64"))
	require.EqualError(t, err, "multi-platform image building not supported when building a local tar image")
}

func TestPlatform_ASingleImageBaseIsForOnePlatform(t *testing.T) {
	base, err := tarOf(t, scratchFor("arm64"))
	require.NoError(t, err)

	_, err = tarOf(t, FromImage(TarSource(base)).AddPlatform("arm64", "linux").AddPlatform("amd64", "linux"))
	require.ErrorContains(t, err, "cannot build for multiple platforms since the base image '"+base+"' is not a manifest list.")
}

func TestPlatform_ASingleImageBaseOfAnotherNamedPlatformIsRefused(t *testing.T) {
	base, err := tarOf(t, scratchFor("amd64"))
	require.NoError(t, err)

	_, err = tarOf(t, FromImage(TarSource(base)).AddPlatform("arm64", "linux"))
	require.ErrorContains(t, err, "the configured platform (arm64/linux) doesn't match the platform (amd64/linux) of the base image ("+base+")")
}

func TestPlatform_ASingleImageBaseIsKeptForTheDefaultPlatform(t *testing.T) {
	base, err := tarOf(t, scratchFor("arm64"))
	require.NoError(t, err)

	for _, b := range []*ContainerBuilder{FromImage(TarSource(base)), FromImage(TarSource(base)).AddPlatform("amd64", "linux")} {
		file, err := tarOf(t, b.SetEntrypoint("/app"))
		require.NoError(t, err)
		img, err := tarball.ImageFromPath(file, nil)
		require.NoError(t, err)
		assert.Equal(t, "linux/arm64", platformOf(t, img))
	}
}

// listBase pushes a manifest list of archs and returns its reference.
func listBase(t *testing.T, archs ...string) string {
	t.Helper()
	ref := serve(t) + "/acme/base:1"
	r, err := name.ParseReference(ref)
	require.NoError(t, err)
	require.NoError(t, remote.WriteIndex(r, index(t, archs...)))
	return ref
}

func TestPlatform_AManifestListBaseGivesThePlatformNamed(t *testing.T) {
	file, err := tarOf(t, From(listBase(t, "amd64", "arm64")).AddPlatform("arm64", "linux").SetEntrypoint("/app"))
	require.NoError(t, err)
	img, err := tarball.ImageFromPath(file, nil)
	require.NoError(t, err)
	assert.Equal(t, "linux/arm64", platformOf(t, img))
}

func TestPlatform_AManifestListWithoutThePlatformIsRefused(t *testing.T) {
	ref := listBase(t, "amd64")
	_, err := tarOf(t, From(ref).AddPlatform("arm64", "linux"))
	require.ErrorContains(t, err, ref+" is a manifest list, but the list does not contain an image for architecture=arm64, os=linux.")
}

// pushedList pushes b to a registry and reads back what its tags name.
func pushedList(t *testing.T, b *ContainerBuilder, tags ...string) (*Container, map[string]*remote.Descriptor) {
	t.Helper()
	ref := serve(t) + "/acme/app:1"
	var opts []ContainerizerOption
	for _, tag := range tags {
		opts = append(opts, WithAdditionalTag(tag))
	}
	c, err := b.Containerize(context.Background(), ToRegistry(ref, opts...))
	require.NoError(t, err)
	out := map[string]*remote.Descriptor{}
	for _, tag := range append([]string{"1"}, tags...) {
		r, err := name.ParseReference(ref[:len(ref)-1] + tag)
		require.NoError(t, err)
		d, err := remote.Get(r)
		require.NoError(t, err)
		out[tag] = d
	}
	return c, out
}

func TestPlatform_SeveralPlatformsArePushedAsAManifestList(t *testing.T) {
	c, got := pushedList(t, FromScratch().AddPlatform("amd64", "linux").AddPlatform("arm64", "linux").SetEntrypoint("/app"), "latest")

	for _, d := range got {
		assert.Equal(t, types.DockerManifestList, d.MediaType)
		assert.Equal(t, c.Digest, d.Digest)
	}
	assert.Equal(t, c.Digest, c.ImageID, "Jib reports the list's digest as the image ID")
	idx, err := got["1"].ImageIndex()
	require.NoError(t, err)
	m, err := idx.IndexManifest()
	require.NoError(t, err)
	require.Len(t, m.Manifests, 2)
	for i, arch := range []string{"amd64", "arm64"} {
		d := m.Manifests[i]
		assert.Equal(t, types.DockerManifestSchema2, d.MediaType)
		assert.Equal(t, &v1.Platform{OS: "linux", Architecture: arch}, d.Platform)
		img, err := idx.Image(d.Digest)
		require.NoError(t, err)
		assert.Equal(t, "linux/"+arch, platformOf(t, img))
	}
}

func TestPlatform_SeveralPlatformsOfAManifestListBaseArePushedAsAManifestList(t *testing.T) {
	_, got := pushedList(t, From(listBase(t, "amd64", "arm64")).AddPlatform("amd64", "linux").AddPlatform("arm64", "linux").SetEntrypoint("/app"))

	idx, err := got["1"].ImageIndex()
	require.NoError(t, err)
	m, err := idx.IndexManifest()
	require.NoError(t, err)
	require.Len(t, m.Manifests, 2)
	img, err := idx.Image(m.Manifests[1].Digest)
	require.NoError(t, err)
	assert.Equal(t, "linux/arm64", platformOf(t, img))
}

// Jib builds no OCI image index; gib pushes OCI-format images of several
// platforms as one.
func TestPlatform_SeveralOCIFormatPlatformsArePushedAsAnOCIIndex(t *testing.T) {
	_, got := pushedList(t, scratchFor("amd64", "arm64").SetFormat(OCIFormat))
	assert.Equal(t, types.OCIImageIndex, got["1"].MediaType)
}
