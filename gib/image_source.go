package gib

import (
	"bytes"
	"context"
	"fmt"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

// ImageSource provides a base image.
type ImageSource interface {
	resolve(ctx context.Context, platform v1.Platform) (v1.Image, error)
	description() string
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

func (s *registrySource) resolve(ctx context.Context, platform v1.Platform) (v1.Image, error) {
	ref, err := name.ParseReference(s.ref, s.nameOptions...)
	if err != nil {
		return nil, err
	}

	// An index resolves to the image for the platform being built.
	opts := []remote.Option{remote.WithContext(ctx), remote.WithPlatform(platform)}
	if s.hasAuth {
		opts = append(opts, s.authOptions...)
	} else {
		opts = append(opts, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	}

	return remote.Image(ref, opts...)
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
// writes an OCI-format image, is read by its index.json: the image it
// names, or the one an index of several holds for platform.
func (s *tarSource) resolve(_ context.Context, platform v1.Platform) (v1.Image, error) {
	img, err := tarball.ImageFromPath(s.path, nil)
	if err == nil {
		return img, nil
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
	return layoutImage(a, m, platform)
}

// layoutImage is the image an OCI image layout's index names, descending
// through nested indexes to the one for platform.
func layoutImage(a *archive, m *v1.IndexManifest, platform v1.Platform) (v1.Image, error) {
	var pick *v1.Descriptor
	for i, d := range m.Manifests {
		if len(m.Manifests) == 1 || d.Platform != nil && d.Platform.Satisfies(platform) {
			pick = &m.Manifests[i]
			break
		}
	}
	if pick == nil {
		return nil, fmt.Errorf("the image layout holds no image for %s/%s", platform.OS, platform.Architecture)
	}
	t, err := a.manifest(*pick)
	if err != nil {
		return nil, err
	}
	switch t := t.(type) {
	case v1.Image:
		return t, nil
	case *rawIndex:
		child, err := t.IndexManifest()
		if err != nil {
			return nil, err
		}
		return layoutImage(a, child, platform)
	}
	return nil, fmt.Errorf("%s is neither an image nor an index", pick.Digest)
}

type scratchSource struct{}

func (s *scratchSource) description() string { return "scratch" }

func (s *scratchSource) resolve(_ context.Context, _ v1.Platform) (v1.Image, error) {
	return empty.Image, nil
}
