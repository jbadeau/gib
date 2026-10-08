package gib

import (
	"context"
	"errors"
	"fmt"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

// ContainerizerOption configures a Containerizer.
type ContainerizerOption func(*Containerizer)

// Containerizer writes a built image to a target.
type Containerizer struct {
	targetType              string // "registry" or "tar"
	registryRef             string
	tarPath                 string
	tarImageName            string
	additionalTags          []string
	credentialHelper        string
	username                string
	password                string
	allowInsecureRegistries bool
	sendCredentialsOverHTTP bool
	remoteOptions           []remote.Option
}

// ToRegistry creates a Containerizer that pushes to a registry.
func ToRegistry(ref string, opts ...ContainerizerOption) *Containerizer {
	c := &Containerizer{
		targetType:  "registry",
		registryRef: ref,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ToTar creates a Containerizer that writes a tar file.
func ToTar(path string, opts ...ContainerizerOption) *Containerizer {
	c := &Containerizer{
		targetType: "tar",
		tarPath:    path,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithAdditionalTag adds an additional tag.
func WithAdditionalTag(tag string) ContainerizerOption {
	return func(c *Containerizer) {
		c.additionalTags = append(c.additionalTags, tag)
	}
}

// WithCredentialHelper sets the credential helper suffix.
func WithCredentialHelper(helper string) ContainerizerOption {
	return func(c *Containerizer) {
		c.credentialHelper = helper
	}
}

// WithCredentials sets explicit username/password credentials.
func WithCredentials(username, password string) ContainerizerOption {
	return func(c *Containerizer) {
		c.username = username
		c.password = password
	}
}

// WithAllowInsecureRegistries allows HTTP registries.
func WithAllowInsecureRegistries(allow bool) ContainerizerOption {
	return func(c *Containerizer) {
		c.allowInsecureRegistries = allow
	}
}

// WithSendCredentialsOverHTTP allows sending credentials over HTTP.
func WithSendCredentialsOverHTTP(allow bool) ContainerizerOption {
	return func(c *Containerizer) {
		c.sendCredentialsOverHTTP = allow
	}
}

// WithTarImageName sets the image name used inside the tar manifest.
func WithTarImageName(imageName string) ContainerizerOption {
	return func(c *Containerizer) {
		c.tarImageName = imageName
	}
}

// Description returns a human-readable description of the target.
func (c *Containerizer) Description() string {
	switch c.targetType {
	case "registry":
		return c.registryRef
	case "tar":
		return c.tarPath
	default:
		return c.targetType
	}
}

// write writes the images built, one per platform. A tarball holds one
// image, as Jib's does; a registry takes several as their manifest list.
func (c *Containerizer) write(ctx context.Context, images []v1.Image) (*Container, error) {
	switch c.targetType {
	case "registry":
		if len(images) > 1 {
			return c.writeManifestList(ctx, images)
		}
		return c.writeRegistry(ctx, images[0])
	case "tar":
		if len(images) > 1 {
			return nil, errors.New("multi-platform image building not supported when building a local tar image")
		}
		return c.writeTar(images[0])
	default:
		return nil, fmt.Errorf("unknown target type: %s", c.targetType)
	}
}

// writeManifestList pushes every image by its digest and, under the
// target's tag and every additional tag, the manifest list naming each
// with its platform, as Jib pushes a multi-platform image. Docker-format
// images make a Docker manifest list; OCI-format images, for which Jib
// builds none, an OCI image index. The list's digest is the container's
// digest and its image ID, as Jib reports it.
func (c *Containerizer) writeManifestList(ctx context.Context, images []v1.Image) (*Container, error) {
	nameOpts := c.nameOptions()
	ref, err := name.ParseReference(c.registryRef, nameOpts...)
	if err != nil {
		return nil, fmt.Errorf("invalid target reference %q: %w", c.registryRef, err)
	}
	opts := c.registryOptions(ctx)

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

	tags := []string{ref.Identifier()}
	tags = append(tags, c.additionalTags...)
	for i, tag := range tags {
		tagRef, err := name.NewTag(ref.Context().String()+":"+tag, nameOpts...)
		if err != nil {
			return nil, fmt.Errorf("invalid tag %q: %w", tag, err)
		}
		if err := remote.WriteIndex(tagRef, list, opts...); err != nil {
			if i == 0 {
				return nil, fmt.Errorf("pushing manifest list: %w", err)
			}
			return nil, fmt.Errorf("pushing additional tag %q: %w", tag, err)
		}
	}

	digest, err := list.Digest()
	if err != nil {
		return nil, err
	}
	return &Container{Digest: digest, ImageID: digest, Tags: tags, TargetImage: ref.String()}, nil
}

func (c *Containerizer) nameOptions() []name.Option {
	if c.allowInsecureRegistries {
		return []name.Option{name.Insecure}
	}
	return nil
}

func (c *Containerizer) writeRegistry(ctx context.Context, image v1.Image) (*Container, error) {
	nameOpts := c.nameOptions()

	ref, err := name.ParseReference(c.registryRef, nameOpts...)
	if err != nil {
		return nil, fmt.Errorf("invalid target reference %q: %w", c.registryRef, err)
	}

	opts := c.registryOptions(ctx)

	// Push the image
	if err := remote.Write(ref, image, opts...); err != nil {
		return nil, fmt.Errorf("pushing image: %w", err)
	}

	// Push additional tags
	for _, tag := range c.additionalTags {
		tagRef, err := name.NewTag(ref.Context().String()+":"+tag, nameOpts...)
		if err != nil {
			return nil, fmt.Errorf("invalid additional tag %q: %w", tag, err)
		}
		if err := remote.Write(tagRef, image, opts...); err != nil {
			return nil, fmt.Errorf("pushing additional tag %q: %w", tag, err)
		}
	}

	digest, err := image.Digest()
	if err != nil {
		return nil, err
	}
	imageID, err := image.ConfigName()
	if err != nil {
		return nil, err
	}

	tags := []string{ref.Identifier()}
	tags = append(tags, c.additionalTags...)

	return &Container{
		Digest:      digest,
		ImageID:     imageID,
		Tags:        tags,
		TargetImage: ref.String(),
	}, nil
}

// registryOptions are the options every request to the target registry
// carries: its credentials, explicit ones first, then the credential
// helper, then the default keychain (Docker's config.json).
func (c *Containerizer) registryOptions(ctx context.Context) []remote.Option {
	opts := []remote.Option{remote.WithContext(ctx)}
	switch {
	case c.username != "" && c.password != "":
		opts = append(opts, remote.WithAuth(&authn.Basic{Username: c.username, Password: c.password}))
	case c.credentialHelper != "":
		kc := newCredentialHelperKeychain(c.credentialHelper)
		opts = append(opts, remote.WithAuthFromKeychain(authn.NewMultiKeychain(kc, authn.DefaultKeychain)))
	default:
		opts = append(opts, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	}
	return append(opts, c.remoteOptions...)
}

func (c *Containerizer) writeTar(image v1.Image) (*Container, error) {
	var ref name.Reference
	var err error
	if c.tarImageName != "" {
		ref, err = name.ParseReference(c.tarImageName)
		if err != nil {
			return nil, fmt.Errorf("invalid tar image name %q: %w", c.tarImageName, err)
		}
	}

	tag, _ := name.NewTag("gib/image:latest")
	if ref != nil {
		if t, ok := ref.(name.Tag); ok {
			tag = t
		} else {
			tag, err = name.NewTag(ref.String())
			if err != nil {
				return nil, fmt.Errorf("invalid tag from reference %q: %w", ref.String(), err)
			}
		}
	}
	if err := writeImageTar(c.tarPath, tag, image); err != nil {
		return nil, fmt.Errorf("writing tar: %w", err)
	}

	digest, err := image.Digest()
	if err != nil {
		return nil, err
	}
	imageID, err := image.ConfigName()
	if err != nil {
		return nil, err
	}

	return &Container{
		Digest:      digest,
		ImageID:     imageID,
		TargetImage: c.tarPath,
	}, nil
}
