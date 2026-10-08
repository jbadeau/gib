package gib

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// ContainerizerOption configures a Containerizer.
type ContainerizerOption func(*Containerizer)

// Containerizer writes a built image to a target: a registry, a tarball
// or the Docker daemon.
type Containerizer struct {
	targetType     string // "registry", "tar" or "docker"
	registryRef    string
	tarPath        string
	tarImageName   string
	additionalTags []string
	creds          credentials
	settings       registrySettings
	remoteOptions  []remote.Option
	// toolName and toolVersion name what built the image in each layer's
	// history, as Jib's Containerizer.setToolName and setToolVersion do.
	toolName    string
	toolVersion string
}

// registrySettings are how gib talks to every registry a build reaches,
// base and target alike, as Jib's Containerizer settings apply to both.
type registrySettings struct {
	allowInsecure           bool
	sendCredentialsOverHTTP bool
	serialize               bool
	log                     LogHandler
	trace                   HTTPTrace
	traceTo                 io.Writer
	mirrors                 map[string][]string
}

func newContainerizer(kind string, opts []ContainerizerOption) *Containerizer {
	c := &Containerizer{targetType: kind, toolName: "gib", toolVersion: Version()}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ToRegistry creates a Containerizer that pushes to a registry. A
// registry:// prefix on ref is dropped, as Jib drops it.
func ToRegistry(ref string, opts ...ContainerizerOption) *Containerizer {
	c := newContainerizer("registry", opts)
	c.registryRef = strings.TrimPrefix(ref, "registry://")
	return c
}

// ToTar creates a Containerizer that writes a tar file.
func ToTar(path string, opts ...ContainerizerOption) *Containerizer {
	c := newContainerizer("tar", opts)
	c.tarPath = path
	return c
}

// ToDocker creates a Containerizer that loads the image into the Docker
// daemon DOCKER_HOST names, as ref.
func ToDocker(ref string, opts ...ContainerizerOption) *Containerizer {
	c := newContainerizer("docker", opts)
	c.registryRef = ref
	return c
}

// WithToolName sets the tool named in each layer's history, "gib" by
// default.
func WithToolName(name string) ContainerizerOption {
	return func(c *Containerizer) { c.toolName = name }
}

// WithToolVersion sets the version of the tool named in each layer's
// history, gib's own by default.
func WithToolVersion(v string) ContainerizerOption {
	return func(c *Containerizer) { c.toolVersion = v }
}

// Version is gib's module version, as the binary or program built with
// it records it.
func Version() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Path == "github.com/jbadeau/gib" && info.Main.Version != "" {
			return info.Main.Version
		}
		for _, d := range info.Deps {
			if d.Path == "github.com/jbadeau/gib" {
				return d.Version
			}
		}
	}
	return "unknown"
}

// WithAdditionalTag adds an additional tag.
func WithAdditionalTag(tag string) ContainerizerOption {
	return func(c *Containerizer) {
		c.additionalTags = append(c.additionalTags, tag)
	}
}

// WithCredentialHelper sets the credential helper for the target
// registry: a path to one, or the suffix of a docker-credential-<suffix>
// on the PATH.
func WithCredentialHelper(helper string) ContainerizerOption {
	return func(c *Containerizer) { c.creds.helper = helper }
}

// WithCredential sets the target registry's credential, logged as
// coming from source.
func WithCredential(cred Credential, source string) ContainerizerOption {
	return func(c *Containerizer) {
		c.creds.known = &cred
		c.creds.knownSource = source
	}
}

// WithCredentials sets explicit username/password credentials.
func WithCredentials(username, password string) ContainerizerOption {
	return WithCredential(Credential{Username: username, Password: password}, "username and password")
}

// WithAllowInsecureRegistries lets gib reach a registry it cannot verify
// over HTTPS without verification, then over plain HTTP, as Jib does.
func WithAllowInsecureRegistries(allow bool) ContainerizerOption {
	return func(c *Containerizer) { c.settings.allowInsecure = allow }
}

// WithSendCredentialsOverHTTP lets credentials be sent over plain HTTP.
func WithSendCredentialsOverHTTP(allow bool) ContainerizerOption {
	return func(c *Containerizer) { c.settings.sendCredentialsOverHTTP = allow }
}

// WithSerialize makes gib push one blob at a time.
func WithSerialize(serialize bool) ContainerizerOption {
	return func(c *Containerizer) { c.settings.serialize = serialize }
}

// WithLogHandler sets what receives gib's log messages.
func WithLogHandler(h LogHandler) ContainerizerOption {
	return func(c *Containerizer) { c.settings.log = h }
}

// WithHTTPTrace traces every HTTP exchange with a registry to w.
func WithHTTPTrace(level HTTPTrace, w io.Writer) ContainerizerOption {
	return func(c *Containerizer) {
		c.settings.trace = level
		c.settings.traceTo = w
	}
}

// WithRegistryMirrors sets the mirrors a base image from registry is
// pulled from first, in order, as Jib's registryMirrors.
func WithRegistryMirrors(registry string, mirrors ...string) ContainerizerOption {
	return func(c *Containerizer) {
		if c.settings.mirrors == nil {
			c.settings.mirrors = map[string][]string{}
		}
		c.settings.mirrors[registry] = append(c.settings.mirrors[registry], mirrors...)
	}
}

// WithTarImageName sets the image name used inside the tar manifest.
func WithTarImageName(imageName string) ContainerizerOption {
	return func(c *Containerizer) { c.tarImageName = imageName }
}

// Description returns a human-readable description of the target.
func (c *Containerizer) Description() string {
	if c.targetType == "tar" {
		return c.tarPath
	}
	return c.registryRef
}

// reference is the image the target names: the registry or Docker
// reference, or a tarball's image name.
func (c *Containerizer) reference() (reference, error) {
	if c.targetType == "tar" {
		if c.tarImageName == "" {
			return parseReference("gib/image")
		}
		return parseReference(c.tarImageName)
	}
	return parseReference(c.registryRef)
}

// Validate fails as Jib fails before it builds anything: for a
// reference it does not parse, a credential helper path that does not
// exist, or an invalid additional tag.
func (c *Containerizer) Validate() error {
	if _, err := c.reference(); err != nil {
		return err
	}
	if c.targetType == "registry" {
		if err := c.creds.check(); err != nil {
			return err
		}
	}
	for _, t := range c.additionalTags {
		if err := validTag(t); err != nil {
			return err
		}
	}
	return nil
}

// tags are every tag the image is written under: the reference's
// qualifier, then each additional tag, each once.
func (c *Containerizer) tags(ref reference) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range append([]string{ref.qualifier()}, c.additionalTags...) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// repoTags are the names a tarball's manifest gives the image, one per
// tag, as Jib's ImageTarball writes them.
func repoTags(ref reference, tags []string) []string {
	out := make([]string, len(tags))
	for i, t := range tags {
		out[i] = ref.withQualifier(t).withQualifierString()
	}
	return out
}

// write writes the images built, one per platform. A tarball holds one
// image, as Jib's does; a registry takes several as their manifest list;
// the Docker daemon takes the one for its platform.
func (c *Containerizer) write(ctx context.Context, images []v1.Image) (*Container, error) {
	ref, err := c.reference()
	if err != nil {
		return nil, err
	}
	tags := c.tags(ref)
	switch c.targetType {
	case "registry":
		if len(images) > 1 {
			return c.writeManifestList(ctx, ref, tags, images)
		}
		return c.writeRegistry(ctx, ref, tags, images[0])
	case "tar":
		if len(images) > 1 {
			return nil, errors.New("multi-platform image building not supported when building a local tar image")
		}
		c.settings.log.log(LevelProgress, "Building image to tar file...")
		if err := writeImageTar(c.tarPath, repoTags(ref, tags), ref.withQualifierString(), images[0]); err != nil {
			return nil, fmt.Errorf("writing tar: %w", err)
		}
		return local(ref, tags, images[0])
	case "docker":
		return c.writeDocker(ctx, ref, tags, images)
	}
	return nil, fmt.Errorf("unknown target type: %s", c.targetType)
}

// local is the container an image written locally makes: not pushed.
func local(ref reference, tags []string, img v1.Image) (*Container, error) {
	digest, err := img.Digest()
	if err != nil {
		return nil, err
	}
	id, err := img.ConfigName()
	if err != nil {
		return nil, err
	}
	return &Container{Digest: digest, ImageID: id, Tags: tags, TargetImage: ref.String()}, nil
}

// nameOptions let go-containerregistry reach the target over plain HTTP
// when insecure registries are allowed.
func (c *Containerizer) nameOptions() []name.Option {
	if c.settings.allowInsecure {
		return []name.Option{name.Insecure}
	}
	return nil
}

// qualified is ref's repository under tag, by digest when tag is one.
func qualified(ref reference, tag string, insecure bool) (name.Reference, error) {
	repo, err := ref.repoName(insecure)
	if err != nil {
		return nil, err
	}
	if digestRE.MatchString(tag) {
		return repo.Digest(tag), nil
	}
	return repo.Tag(tag), nil
}

// writeManifestList pushes every image by its digest and, under every
// tag, the manifest list naming each with its platform, as Jib pushes a
// multi-platform image. Docker-format images make a Docker manifest
// list; OCI-format images, for which Jib builds none, an OCI image
// index. The list's digest is the container's digest and its image ID,
// as Jib reports it.
func (c *Containerizer) writeManifestList(ctx context.Context, ref reference, tags []string, images []v1.Image) (*Container, error) {
	opts := c.registryOptions(ctx, ref)
	listType := types.DockerManifestList
	var adds []mutate.IndexAddendum
	for _, img := range images {
		mt, err := img.MediaType()
		if err != nil {
			return nil, err
		}
		if mt == types.OCIManifestSchema1 {
			listType = types.OCIImageIndex
		}
		cfg, err := img.ConfigFile()
		if err != nil {
			return nil, err
		}
		adds = append(adds, mutate.IndexAddendum{Add: img, Descriptor: v1.Descriptor{
			MediaType: mt,
			Platform:  &v1.Platform{Architecture: cfg.Architecture, OS: cfg.OS},
		}})
	}
	list := mutate.AppendManifests(mutate.IndexMediaType(empty.Index, listType), adds...)
	for _, tag := range tags {
		r, err := qualified(ref, tag, c.settings.allowInsecure)
		if err != nil {
			return nil, err
		}
		if err := remote.WriteIndex(r, list, opts...); err != nil {
			return nil, fmt.Errorf("pushing manifest list: %w", err)
		}
	}
	digest, err := list.Digest()
	if err != nil {
		return nil, err
	}
	return &Container{Digest: digest, ImageID: digest, Tags: tags, TargetImage: ref.String(), ImagePushed: true}, nil
}

func (c *Containerizer) writeRegistry(ctx context.Context, ref reference, tags []string, image v1.Image) (*Container, error) {
	opts := c.registryOptions(ctx, ref)
	for _, tag := range tags {
		r, err := qualified(ref, tag, c.settings.allowInsecure)
		if err != nil {
			return nil, err
		}
		if err := remote.Write(r, image, opts...); err != nil {
			return nil, fmt.Errorf("pushing image: %w", err)
		}
	}
	out, err := local(ref, tags, image)
	if err != nil {
		return nil, err
	}
	out.ImagePushed = true
	return out, nil
}

// registryOptions are the options every request to ref's registry
// carries: how it is reached, and its credentials, found as Jib finds
// them.
func (c *Containerizer) registryOptions(ctx context.Context, ref reference) []remote.Option {
	auth := remote.WithAuthFromKeychain(c.creds.keychain(ref, c.settings.log))
	return append(c.settings.options(ctx, ref, auth), c.remoteOptions...)
}

// options are the options a request to ref's registry carries, with
// auth its credentials.
func (s registrySettings) options(ctx context.Context, ref reference, auth remote.Option) []remote.Option {
	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithTransport(newGuard(ref, s)),
		auth,
	}
	if s.serialize {
		opts = append(opts, remote.WithJobs(1))
	}
	return opts
}
