package gib

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// ImageSource provides a base image.
type ImageSource interface {
	resolve(ctx context.Context, platforms []v1.Platform, settings registrySettings) ([]base, error)
	check() error
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

// WithSourceCredential sets the base image registry's credential, logged
// as coming from source.
func WithSourceCredential(cred Credential, source string) ImageSourceOption {
	return func(s *registrySource) {
		s.creds.known = &cred
		s.creds.knownSource = source
	}
}

// WithSourceCredentials sets explicit username/password credentials for the base image registry.
func WithSourceCredentials(username, password string) ImageSourceOption {
	return WithSourceCredential(Credential{Username: username, Password: password}, "username and password")
}

// WithSourceCredentialHelper sets the credential helper for the base
// image registry: a path to one, or the suffix of a
// docker-credential-<suffix> on the PATH.
func WithSourceCredentialHelper(helper string) ImageSourceOption {
	return func(s *registrySource) { s.creds.helper = helper }
}

// WithSourceInsecure lets the base image registry be reached as
// WithAllowInsecureRegistries lets every registry be.
func WithSourceInsecure() ImageSourceOption {
	return func(s *registrySource) { s.insecure = true }
}

type registrySource struct {
	ref      string
	creds    credentials
	insecure bool
}

// RegistrySource creates an ImageSource that pulls from a registry. A
// registry:// prefix on ref is dropped, as Jib drops it.
func RegistrySource(ref string, opts ...ImageSourceOption) ImageSource {
	s := &registrySource{ref: strings.TrimPrefix(ref, "registry://")}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *registrySource) description() string { return s.ref }

// check fails, as Jib does before it builds anything, for a reference
// it does not parse or a credential helper path that does not exist.
func (s *registrySource) check() error {
	if _, err := parseReference(s.ref); err != nil {
		return err
	}
	return s.creds.check()
}

// resolve pulls the base image's manifest as Jib's PullBaseImageStep
// does: from each mirror of its registry first, then from the registry
// without credentials, and only when that is refused, with them.
func (s *registrySource) resolve(ctx context.Context, platforms []v1.Platform, settings registrySettings) ([]base, error) {
	ref, err := parseReference(s.ref)
	if err != nil {
		return nil, err
	}
	settings.allowInsecure = settings.allowInsecure || s.insecure
	log := settings.log
	log.log(LevelProgress, "Getting manifest for base image %s...", ref)

	desc, err := s.fromMirrors(ctx, ref, settings)
	if desc == nil {
		desc, err = get(ref, settings.options(ctx, ref, remote.WithAuth(authn.Anonymous)))
		if unauthorized(err) {
			log.log(LevelLifecycle, "The base image requires auth. Trying again for %s...", ref)
			auth := remote.WithAuthFromKeychain(&keychain{rs: s.creds.retrievers(ref, log)})
			desc, err = get(ref, settings.options(ctx, ref, auth))
		}
	}
	if err != nil {
		return nil, err
	}
	log.log(LevelLifecycle, "Using base image with digest: %s", desc.Digest)
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

// fromMirrors is the base image's manifest from the first mirror of its
// registry that has it, without credentials, or nil.
func (s *registrySource) fromMirrors(ctx context.Context, ref reference, settings registrySettings) (*remote.Descriptor, error) {
	for _, m := range settings.mirrors[ref.registry] {
		settings.log.log(LevelDebug, "mirror config: %s --> %s", ref.registry, m)
		settings.log.log(LevelInfo, "trying mirror %s for the base image", m)
		mr := ref
		mr.registry = m
		desc, err := get(mr, settings.options(ctx, mr, remote.WithAuth(authn.Anonymous)))
		if err != nil {
			settings.log.log(LevelDebug, "failed to get manifest from mirror %s: %s", m, err)
			continue
		}
		settings.log.log(LevelInfo, "pulled manifest from mirror %s", m)
		return desc, nil
	}
	return nil, nil
}

func get(ref reference, opts []remote.Option) (*remote.Descriptor, error) {
	n, err := ref.name()
	if err != nil {
		return nil, err
	}
	return remote.Get(n, opts...)
}

// unauthorized reports whether a registry refused err's request for its
// credentials: 401 or 403, as Jib's RegistryUnauthorizedException.
func unauthorized(err error) bool {
	var t *transport.Error
	return errors.As(err, &t) && (t.StatusCode == http.StatusUnauthorized || t.StatusCode == http.StatusForbidden)
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
func (s *tarSource) check() error { return nil }

func (s *tarSource) resolve(_ context.Context, platforms []v1.Platform, _ registrySettings) ([]base, error) {
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

func (s *scratchSource) check() error { return nil }

// resolve is an empty image for every platform, as Jib builds from
// scratch: each image takes its platform from what it is built for.
func (s *scratchSource) resolve(_ context.Context, platforms []v1.Platform, settings registrySettings) ([]base, error) {
	settings.log.log(LevelProgress, "Getting scratch base image...")
	out := make([]base, len(platforms))
	for i, p := range platforms {
		out[i] = base{image: empty.Image, platform: p}
	}
	return out, nil
}

type dockerSource struct {
	ref string
}

// DockerDaemonSource creates an ImageSource that reads an image from the
// Docker daemon, as Jib reads one it has the daemon save.
func DockerDaemonSource(ref string) ImageSource {
	return &dockerSource{ref: ref}
}

func (s *dockerSource) description() string { return s.ref }

func (s *dockerSource) check() error {
	_, err := name.ParseReference(s.ref)
	return err
}

func (s *dockerSource) resolve(ctx context.Context, platforms []v1.Platform, settings registrySettings) ([]base, error) {
	settings.log.log(LevelProgress, "Getting image from Docker daemon...")
	ref, err := name.ParseReference(s.ref)
	if err != nil {
		return nil, err
	}
	img, err := daemon.Image(ref, daemon.WithContext(ctx), daemon.WithFileBufferedOpener())
	if err != nil {
		return nil, err
	}
	return single(saved{img}, platforms, s.ref)
}

// saved is an image the daemon saved, its config the one it saved
// rather than the one the daemon describes.
type saved struct{ v1.Image }

func (s saved) ConfigFile() (*v1.ConfigFile, error) {
	raw, err := s.RawConfigFile()
	if err != nil {
		return nil, err
	}
	return v1.ParseConfigFile(bytes.NewReader(raw))
}
