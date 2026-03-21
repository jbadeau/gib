package gib

import (
	"context"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

// ImageSource provides a base image.
type ImageSource interface {
	resolve(ctx context.Context) (v1.Image, error)
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

func (s *registrySource) resolve(ctx context.Context) (v1.Image, error) {
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

func (s *tarSource) resolve(_ context.Context) (v1.Image, error) {
	return tarball.ImageFromPath(s.path, nil)
}

type scratchSource struct{}

func (s *scratchSource) description() string { return "scratch" }

func (s *scratchSource) resolve(_ context.Context) (v1.Image, error) {
	return empty.Image, nil
}
