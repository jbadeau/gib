package gib

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

// ImageSource provides a base image.
type ImageSource interface {
	resolve(ctx context.Context, platforms []v1.Platform) ([]base, error)
	description() string
}

// base is the image one platform's image is built on.
type base struct {
	image    v1.Image
	platform v1.Platform
}

// single is the base a single image is for platforms, as Jib's
// PlatformChecker allows it: only for one platform, and only for the one
// it was built for, unless that platform is linux/amd64, which is Jib's
// default and so may not have been named at all.
func single(img v1.Image, platforms []v1.Platform, name string) ([]base, error) {
	if len(platforms) != 1 {
		return nil, fmt.Errorf("cannot build for multiple platforms since the base image '%s' is not a manifest list.", name) //nolint:staticcheck // Jib's message, word for word
	}
	p := platforms[0]
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}
	if (p.Architecture != cfg.Architecture || p.OS != cfg.OS) && (p.Architecture != "amd64" || p.OS != "linux") {
		return nil, fmt.Errorf("the configured platform (%s/%s) doesn't match the platform (%s/%s) of the base image (%s)",
			p.Architecture, p.OS, cfg.Architecture, cfg.OS, name)
	}
	return []base{{image: img, platform: p}}, nil
}

// listed are the images a manifest list holds for platforms, matched by
// architecture and OS as Jib matches them.
func listed(idx v1.ImageIndex, platforms []v1.Platform, name string) ([]base, error) {
	m, err := idx.IndexManifest()
	if err != nil {
		return nil, err
	}
	var out []base
	for _, p := range platforms {
		var found *v1.Descriptor
		for i, d := range m.Manifests {
			if d.Platform != nil && d.Platform.OS == p.OS && d.Platform.Architecture == p.Architecture {
				found = &m.Manifests[i]
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("%s is a manifest list, but the list does not contain an image for architecture=%s, os=%s. "+
				"If your intention was to specify a platform for your image, see "+
				"https://github.com/GoogleContainerTools/jib/blob/master/docs/faq.md#how-do-i-specify-a-platform-in-the-manifest-list-or-oci-index-of-a-base-image",
				name, p.Architecture, p.OS)
		}
		img, err := idx.Image(found.Digest)
		if err != nil {
			return nil, err
		}
		out = append(out, base{image: img, platform: p})
	}
	return out, nil
}

// ImageSourceOption configures a registry-based ImageSource.
type ImageSourceOption func(*registrySource)

// WithSourceCredentials sets explicit username/password credentials for the base image registry.
func WithSourceCredentials(username, password string) ImageSourceOption {
	return func(s *registrySource) {
		s.authOptions = append(s.authOptions, remote.WithAuth(&authn.Basic{
			Username: username,
			Password: password,
		}))
		s.hasAuth = true
	}
}

// WithSourceCredentialHelper sets the credential helper suffix for the base image registry.
func WithSourceCredentialHelper(suffix string) ImageSourceOption {
	return func(s *registrySource) {
		kc := newCredentialHelperKeychain(suffix)
		s.authOptions = append(s.authOptions, remote.WithAuthFromKeychain(
			authn.NewMultiKeychain(kc, authn.DefaultKeychain),
		))
		s.hasAuth = true
	}
}

// WithSourceInsecure allows HTTP (non-TLS) connections to the base image registry.
func WithSourceInsecure() ImageSourceOption {
	return func(s *registrySource) {
		s.nameOptions = append(s.nameOptions, name.Insecure)
	}
}

type registrySource struct {
	ref         string
	nameOptions []name.Option
	authOptions []remote.Option
	hasAuth     bool
}

// RegistrySource creates an ImageSource that pulls from a registry.
func RegistrySource(ref string, opts ...ImageSourceOption) ImageSource {
	s := &registrySource{ref: ref}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *registrySource) description() string { return s.ref }

func (s *registrySource) resolve(ctx context.Context, platforms []v1.Platform) ([]base, error) {
	ref, err := name.ParseReference(s.ref, s.nameOptions...)
	if err != nil {
		return nil, err
	}

	opts := []remote.Option{remote.WithContext(ctx)}
	if s.hasAuth {
		opts = append(opts, s.authOptions...)
	} else {
		opts = append(opts, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	}

	desc, err := remote.Get(ref, opts...)
	if err != nil {
		return nil, err
	}
	if desc.MediaType.IsIndex() {
		idx, err := desc.ImageIndex()
		if err != nil {
			return nil, err
		}
		return listed(idx, platforms, s.ref)
	}
	img, err := desc.Image()
	if err != nil {
		return nil, err
	}
	return single(img, platforms, s.ref)
}

type tarSource struct {
	path string
}

// TarSource creates an ImageSource that reads from a tar file.
func TarSource(path string) ImageSource {
	return &tarSource{path: path}
}

func (s *tarSource) description() string { return s.path }

// resolve reads a docker-save archive as Jib does, by its
// manifest.json. A tarball holding only an OCI image layout, as gib
// writes an OCI-format image, is read by its index.json: an index of
// several images is a manifest list, any other a single image.
func (s *tarSource) resolve(_ context.Context, platforms []v1.Platform) ([]base, error) {
	img, err := tarball.ImageFromPath(s.path, nil)
	if err == nil {
		return single(img, platforms, s.path)
	}
	a, aerr := openArchive(s.path)
	if aerr != nil {
		return nil, err
	}
	idx, ok := a.files["index.json"]
	if _, docker := a.files["manifest.json"]; docker || !ok {
		return nil, err
	}
	raw, err := a.read(idx)
	if err != nil {
		return nil, err
	}
	m, err := v1.ParseIndexManifest(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("index.json: %w", err)
	}
	return layoutBases(a, m, platforms, s.path)
}

// layoutBases are the bases an OCI image layout's index names: the one
// image it names, or what the index it names holds for platforms.
func layoutBases(a *archive, m *v1.IndexManifest, platforms []v1.Platform, name string) ([]base, error) {
	if len(m.Manifests) != 1 {
		return listed(&rawIndex{a: a, raw: mustJSON(m), mediaType: types.OCIImageIndex}, platforms, name)
	}
	t, err := a.manifest(m.Manifests[0])
	if err != nil {
		return nil, err
	}
	switch t := t.(type) {
	case v1.Image:
		return single(t, platforms, name)
	case *rawIndex:
		return listed(t, platforms, name)
	}
	return nil, fmt.Errorf("%s is neither an image nor an index", m.Manifests[0].Digest)
}

func mustJSON(m *v1.IndexManifest) []byte {
	b, _ := json.Marshal(m)
	return b
}

type scratchSource struct{}

func (s *scratchSource) description() string { return "scratch" }

// resolve is an empty image for every platform, as Jib builds from
// scratch: each image takes its platform from what it is built for.
func (s *scratchSource) resolve(_ context.Context, platforms []v1.Platform) ([]base, error) {
	out := make([]base, len(platforms))
	for i, p := range platforms {
		out[i] = base{image: empty.Image, platform: p}
	}
	return out, nil
}
