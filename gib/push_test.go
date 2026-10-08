package gib

import (
	"archive/tar"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/google/go-containerregistry/pkg/v1/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serve starts an in-process registry and returns its host.
func serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

// pushed is the digest the registry reports for ref.
func pushed(t *testing.T, ref string) v1.Hash {
	t.Helper()
	r, err := name.ParseReference(ref)
	require.NoError(t, err)
	d, err := remote.Head(r)
	require.NoError(t, err)
	return d.Digest
}

func push(t *testing.T, tarPath, ref string) (*Container, error) {
	t.Helper()
	return ToRegistry(ref, WithAllowInsecureRegistries(true)).Push(context.Background(), tarPath)
}

func ociImage(t *testing.T, arch string) v1.Image {
	t.Helper()
	img, err := random.Image(256, 2)
	require.NoError(t, err)
	img = mutate.MediaType(img, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, types.OCIConfigJSON)
	cfg, err := img.ConfigFile()
	require.NoError(t, err)
	cfg = cfg.DeepCopy()
	cfg.Architecture, cfg.OS = arch, "linux"
	img, err = mutate.ConfigFile(img, cfg)
	require.NoError(t, err)
	return img
}

func index(t *testing.T, archs ...string) v1.ImageIndex {
	t.Helper()
	var adds []mutate.IndexAddendum
	for _, a := range archs {
		adds = append(adds, mutate.IndexAddendum{
			Add:        ociImage(t, a),
			Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: a}},
		})
	}
	return mutate.AppendManifests(mutate.IndexMediaType(empty.Index, types.OCIImageIndex), adds...)
}

// tarDir tars a directory the way an OCI image layout is shipped.
func tarDir(t *testing.T, dir string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "image.tar")
	f, err := os.Create(out)
	require.NoError(t, err)
	tw := tar.NewWriter(f)
	require.NoError(t, filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel), Size: int64(len(data)), Mode: 0o644}); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	}))
	require.NoError(t, tw.Close())
	require.NoError(t, f.Close())
	return out
}

// apkoTar writes idx the way apko writes an image of several
// architectures: index.json is the index itself, manifests and configs
// beside it named by digest, layers by their hex.
func apkoTar(t *testing.T, idx v1.ImageIndex) string {
	t.Helper()
	dir := t.TempDir()
	raw, err := idx.RawManifest()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.json"), raw, 0o644))
	m, err := idx.IndexManifest()
	require.NoError(t, err)
	for _, d := range m.Manifests {
		img, err := idx.Image(d.Digest)
		require.NoError(t, err)
		mraw, err := img.RawManifest()
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, d.Digest.String()), mraw, 0o644))
		cfg, err := img.RawConfigFile()
		require.NoError(t, err)
		cn, err := img.ConfigName()
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, cn.String()), cfg, 0o644))
		layers, err := img.Layers()
		require.NoError(t, err)
		for _, l := range layers {
			h, err := l.Digest()
			require.NoError(t, err)
			rc, err := l.Compressed()
			require.NoError(t, err)
			f, err := os.Create(filepath.Join(dir, h.Hex+".tar.gz"))
			require.NoError(t, err)
			_, err = f.ReadFrom(rc)
			require.NoError(t, err)
			require.NoError(t, f.Close())
		}
	}
	return tarDir(t, dir)
}

func TestPush_SingleArchOCILayoutIsPushedAsItsImage(t *testing.T) {
	img := ociImage(t, "amd64")
	dir := t.TempDir()
	p, err := layout.Write(dir, empty.Index)
	require.NoError(t, err)
	require.NoError(t, p.AppendImage(img))
	ref := serve(t) + "/acme/base:1.4.0"

	got, err := push(t, tarDir(t, dir), ref)
	require.NoError(t, err)

	want, err := img.Digest()
	require.NoError(t, err)
	assert.Equal(t, want, got.Digest)
	assert.Equal(t, want, pushed(t, ref))
}

func TestPush_MultiArchLayoutIsPushedAsItsIndex(t *testing.T) {
	idx := index(t, "amd64", "arm64")
	dir := t.TempDir()
	p, err := layout.Write(dir, empty.Index)
	require.NoError(t, err)
	require.NoError(t, p.AppendIndex(idx))
	ref := serve(t) + "/acme/base:1.4.0"

	got, err := push(t, tarDir(t, dir), ref)
	require.NoError(t, err)

	want, err := idx.Digest()
	require.NoError(t, err)
	assert.Equal(t, want, got.Digest)
	assert.Equal(t, want, pushed(t, ref))
}

func TestPush_ApkoMultiArchTarIsPushedAsItsIndex(t *testing.T) {
	idx := index(t, "amd64", "arm64")
	ref := serve(t) + "/acme/base:1.4.0"

	_, err := push(t, apkoTar(t, idx), ref)
	require.NoError(t, err)

	want, err := idx.Digest()
	require.NoError(t, err)
	assert.Equal(t, want, pushed(t, ref))
	r, _ := name.ParseReference(ref)
	pulled, err := remote.Index(r)
	require.NoError(t, err)
	m, err := pulled.IndexManifest()
	require.NoError(t, err)
	for _, d := range m.Manifests {
		child, err := pulled.Image(d.Digest)
		require.NoError(t, err)
		require.NoError(t, validate.Image(child), "every child of the index is pushed whole")
	}
}

func TestPush_ApkoSingleArchTarIsPushedAsItsImage(t *testing.T) {
	idx := index(t, "amd64")
	m, err := idx.IndexManifest()
	require.NoError(t, err)
	ref := serve(t) + "/acme/base:1.4.0"

	_, err = push(t, apkoTar(t, idx), ref)
	require.NoError(t, err)

	assert.Equal(t, m.Manifests[0].Digest, pushed(t, ref))
}

func TestPush_DockerTarballIsPushedUnderTheManifestDescribingIt(t *testing.T) {
	img, err := random.Image(256, 2)
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "image.tar")
	tag, _ := name.NewTag("acme/app:latest")
	require.NoError(t, tarball.WriteToFile(file, tag, img))
	ref := serve(t) + "/acme/app:1.4.0"

	got, err := push(t, file, ref)
	require.NoError(t, err)

	want, err := img.Digest()
	require.NoError(t, err)
	assert.Equal(t, want, got.Digest)
	assert.Equal(t, want, pushed(t, ref))
}

func TestPush_GibsOwnTarIsPushedWithTheDigestItsBuildReports(t *testing.T) {
	src := filepath.Join(t.TempDir(), "hello.txt")
	require.NoError(t, os.WriteFile(src, []byte("hello"), 0o644))
	for _, format := range []ImageFormat{DockerFormat, OCIFormat} {
		t.Run(format.String(), func(t *testing.T) {
			l, err := NewFileEntriesLayerBuilder().SetName("app").AddEntry(src, "/app/hello.txt").Build()
			require.NoError(t, err)
			file := filepath.Join(t.TempDir(), "image.tar")
			built, err := FromScratch().AddFileEntriesLayer(l).SetFormat(format).
				Containerize(context.Background(), ToTar(file, WithTarImageName("acme/app:latest")))
			require.NoError(t, err)
			ref := serve(t) + "/acme/app:1.4.0"

			_, err = push(t, file, ref)
			require.NoError(t, err)

			assert.Equal(t, built.Digest, pushed(t, ref))
		})
	}
}

func TestPush_AnImageBuiltForAPlatformIsPushedForIt(t *testing.T) {
	for _, format := range []ImageFormat{DockerFormat, OCIFormat} {
		t.Run(format.String(), func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "image.tar")
			built, err := FromScratch().AddPlatform("arm64", "linux").SetEntrypoint("/app").SetFormat(format).
				Containerize(context.Background(), ToTar(file, WithTarImageName("acme/app:latest")))
			require.NoError(t, err)
			ref := serve(t) + "/acme/app:1.4.0"

			_, err = push(t, file, ref)
			require.NoError(t, err)

			r, err := name.ParseReference(ref)
			require.NoError(t, err)
			img, err := remote.Image(r)
			require.NoError(t, err)
			digest, err := img.Digest()
			require.NoError(t, err)
			assert.Equal(t, built.Digest, digest)
			cfg, err := img.ConfigFile()
			require.NoError(t, err)
			assert.Equal(t, "linux/arm64", cfg.OS+"/"+cfg.Architecture)
		})
	}
}

func TestGibsTarIsLoadableAsADockerSaveArchive(t *testing.T) {
	src := filepath.Join(t.TempDir(), "hello.txt")
	require.NoError(t, os.WriteFile(src, []byte("hello"), 0o644))
	l, err := NewFileEntriesLayerBuilder().SetName("app").AddEntry(src, "/app/hello.txt").Build()
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "image.tar")
	built, err := FromScratch().AddFileEntriesLayer(l).Containerize(context.Background(), ToTar(file))
	require.NoError(t, err)

	img, err := tarball.ImageFromPath(file, nil)
	require.NoError(t, err)

	id, err := img.ConfigName()
	require.NoError(t, err)
	assert.Equal(t, built.ImageID, id)
	digest, err := img.Digest()
	require.NoError(t, err)
	assert.Equal(t, built.Digest, digest, "a Docker-format image reads back as the manifest it was built with")
}

func TestPush_RefusesAReferenceWithADigest(t *testing.T) {
	_, err := push(t, "unused.tar", serve(t)+"/acme/app@sha256:"+strings.Repeat("a", 64))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names a digest")
}

func TestPush_RefusesSeveralTaggedImagesInOtherRepositories(t *testing.T) {
	file := filepath.Join(t.TempDir(), "images.tar")
	a, _ := random.Image(64, 1)
	b, _ := random.Image(64, 1)
	ta, _ := name.NewTag("acme/a:1")
	tb, _ := name.NewTag("acme/b:1")
	require.NoError(t, tarball.MultiRefWriteToFile(file, map[name.Reference]v1.Image{ta: a, tb: b}))

	_, err := push(t, file, serve(t)+"/acme/c:1.4.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "push one image per tarball")
}

func TestPush_PicksTheOneImageTaggedInTheReferencesRepository(t *testing.T) {
	host := serve(t)
	file := filepath.Join(t.TempDir(), "images.tar")
	a, _ := random.Image(64, 1)
	b, _ := random.Image(64, 1)
	ta, _ := name.NewTag(host + "/acme/a:dev")
	tb, _ := name.NewTag(host + "/acme/b:dev")
	require.NoError(t, tarball.MultiRefWriteToFile(file, map[name.Reference]v1.Image{ta: a, tb: b}))

	_, err := push(t, file, host+"/acme/b:1.4.0")
	require.NoError(t, err)

	want, _ := b.Digest()
	assert.Equal(t, want, pushed(t, host+"/acme/b:1.4.0"))
}
