package gib

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

const refName = "org.opencontainers.image.ref.name"

// Push pushes the image a tarball holds to the containerizer's registry
// reference, as the tarball holds it: every blob and every manifest with
// its exact bytes, so the digest the registry reports is the one the
// tarball was written with.
//
// A tarball with an index.json is read as an OCI image layout, blobs
// found by their content wherever the tarball keeps them. An index.json
// naming one manifest has that manifest pushed; one naming several
// untagged manifests is itself an image index, as apko writes for
// several architectures, and is pushed as one. A tarball with only a
// manifest.json is a docker-save archive: its config and layers are
// pushed as they are, under the Docker schema 2 manifest that describes
// them.
//
// The reference names a tag; one with a digest is refused, since the
// digest is the tarball's. A tarball holding several tagged images is
// refused unless exactly one of them is tagged in the reference's
// repository.
func (c *Containerizer) Push(ctx context.Context, tarPath string) (*Container, error) {
	if c.targetType != "registry" {
		return nil, fmt.Errorf("push needs a registry target, not %s", c.targetType)
	}
	jref, err := parseReference(c.registryRef)
	if err != nil {
		return nil, err
	}
	if err := c.creds.check(); err != nil {
		return nil, err
	}
	ref, err := name.ParseReference(c.registryRef)
	if err != nil {
		return nil, fmt.Errorf("invalid target reference %q: %w", c.registryRef, err)
	}
	tag, ok := ref.(name.Tag)
	if !ok {
		return nil, fmt.Errorf("target reference %q names a digest; a push names a tag, the digest is the tarball's", c.registryRef)
	}

	a, err := openArchive(tarPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = a.f.Close() }()

	t, err := a.taggable(tag)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", tarPath, err)
	}

	opts := c.registryOptions(ctx, jref)
	switch t := t.(type) {
	case v1.ImageIndex:
		err = remote.WriteIndex(tag, t, opts...)
	case v1.Image:
		err = remote.Write(tag, t, opts...)
	}
	if err != nil {
		return nil, fmt.Errorf("pushing %s: %w", tarPath, err)
	}
	tags := []string{tag.TagStr()}
	for _, extra := range c.additionalTags {
		et, err := name.NewTag(tag.Context().String() + ":" + extra)
		if err != nil {
			return nil, fmt.Errorf("invalid additional tag %q: %w", extra, err)
		}
		if err := remote.Tag(et, t, opts...); err != nil {
			return nil, fmt.Errorf("tagging %s: %w", et, err)
		}
		tags = append(tags, extra)
	}

	digest, err := t.Digest()
	if err != nil {
		return nil, err
	}
	out := &Container{Digest: digest, Tags: tags, TargetImage: tag.String(), ImagePushed: true}
	if img, ok := t.(v1.Image); ok {
		if out.ImageID, err = img.ConfigName(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

type pushable interface {
	remote.Taggable
	Digest() (v1.Hash, error)
}

// archive is an image tarball, its regular files found by name and by
// the digest of their content.
type archive struct {
	path  string
	f     *os.File
	files map[string]*io.SectionReader
	blobs map[v1.Hash]*io.SectionReader
}

func openArchive(file string) (*archive, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	a := &archive{path: file, f: f, files: map[string]*io.SectionReader{}, blobs: map[v1.Hash]*io.SectionReader{}}
	pos := &counter{r: f}
	tr := tar.NewReader(pos)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("reading %s: %w", file, err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		start := pos.n
		sum := sha256.New()
		if _, err := io.Copy(sum, tr); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("reading %s: %w", file, err)
		}
		s := io.NewSectionReader(f, start, h.Size)
		a.files[strings.TrimPrefix(h.Name, "./")] = s
		a.blobs[v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(sum.Sum(nil))}] = s
	}
	return a, nil
}

// counter counts what archive/tar reads, which never reads ahead of the
// entry it returns: after Next the count is where the entry's data
// starts.
type counter struct {
	r io.Reader
	n int64
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (a *archive) read(s *io.SectionReader) ([]byte, error) {
	return io.ReadAll(io.NewSectionReader(s, 0, s.Size()))
}

func (a *archive) blob(h v1.Hash) ([]byte, error) {
	s, ok := a.blobs[h]
	if !ok {
		return nil, fmt.Errorf("holds no blob %s", h)
	}
	return a.read(s)
}

// taggable is what the tarball holds for tag: an image or an index.
func (a *archive) taggable(tag name.Tag) (pushable, error) {
	if s, ok := a.files["index.json"]; ok {
		raw, err := a.read(s)
		if err != nil {
			return nil, err
		}
		return a.fromIndex(raw, tag)
	}
	if _, ok := a.files["manifest.json"]; ok {
		return a.fromDocker(tag)
	}
	return nil, errors.New("holds neither an index.json nor a manifest.json")
}

func (a *archive) fromIndex(raw []byte, tag name.Tag) (pushable, error) {
	idx, err := v1.ParseIndexManifest(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("index.json: %w", err)
	}
	var d v1.Descriptor
	switch named := tagged(idx.Manifests); {
	case len(idx.Manifests) == 0:
		return nil, errors.New("index.json names no image")
	case len(idx.Manifests) == 1:
		d = idx.Manifests[0]
	case len(named) == 0:
		return &rawIndex{a: a, raw: raw, mediaType: orDefault(idx.MediaType, types.OCIImageIndex)}, nil
	default:
		var in []v1.Descriptor
		for _, m := range named {
			if sameRepository(m.Annotations["io.containerd.image.name"], tag) || sameRepository(m.Annotations[refName], tag) {
				in = append(in, m)
			}
		}
		if len(in) != 1 {
			return nil, fmt.Errorf("index.json names %d tagged images, %d of them in %s; push one image per tarball", len(named), len(in), tag.Context())
		}
		d = in[0]
	}
	return a.manifest(d)
}

// manifest is the image or index d describes, with the bytes the
// tarball holds for it.
func (a *archive) manifest(d v1.Descriptor) (pushable, error) {
	raw, err := a.blob(d.Digest)
	if err != nil {
		return nil, err
	}
	switch {
	case d.MediaType.IsIndex():
		return &rawIndex{a: a, raw: raw, mediaType: d.MediaType}, nil
	case d.MediaType.IsImage():
		return partial.CompressedToImage(&rawImage{a: a, raw: raw, mediaType: d.MediaType})
	}
	return nil, fmt.Errorf("%s is a %s, neither an image nor an index", d.Digest, d.MediaType)
}

func (a *archive) fromDocker(tag name.Tag) (pushable, error) {
	raw, err := a.read(a.files["manifest.json"])
	if err != nil {
		return nil, err
	}
	var images []dockerImage
	if err := json.Unmarshal(raw, &images); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	var pick *name.Tag
	if len(images) > 1 {
		var in []string
		for _, img := range images {
			for _, t := range img.RepoTags {
				if sameRepository(t, tag) {
					in = append(in, t)
					break
				}
			}
		}
		if len(in) != 1 {
			return nil, fmt.Errorf("manifest.json names %d images, %d of them in %s; push one image per tarball", len(images), len(in), tag.Context())
		}
		t, err := name.NewTag(in[0])
		if err != nil {
			return nil, err
		}
		pick = &t
	}
	return tarball.Image(func() (io.ReadCloser, error) { return os.Open(a.path) }, pick)
}

func tagged(ds []v1.Descriptor) []v1.Descriptor {
	var out []v1.Descriptor
	for _, d := range ds {
		if d.Annotations[refName] != "" || d.Annotations["io.containerd.image.name"] != "" {
			out = append(out, d)
		}
	}
	return out
}

// sameRepository reports whether s is a full reference into tag's
// repository.
func sameRepository(s string, tag name.Tag) bool {
	if s == "" {
		return false
	}
	ref, err := name.ParseReference(s)
	return err == nil && ref.Context().Name() == tag.Context().Name()
}

func orDefault(mt, def types.MediaType) types.MediaType {
	if mt == "" {
		return def
	}
	return mt
}

// rawImage is an image manifest exactly as the tarball holds it.
type rawImage struct {
	a         *archive
	raw       []byte
	mediaType types.MediaType
	m         *v1.Manifest
}

func (i *rawImage) RawManifest() ([]byte, error)        { return i.raw, nil }
func (i *rawImage) MediaType() (types.MediaType, error) { return i.mediaType, nil }

func (i *rawImage) manifest() (*v1.Manifest, error) {
	if i.m == nil {
		m, err := v1.ParseManifest(bytes.NewReader(i.raw))
		if err != nil {
			return nil, err
		}
		i.m = m
	}
	return i.m, nil
}

func (i *rawImage) RawConfigFile() ([]byte, error) {
	m, err := i.manifest()
	if err != nil {
		return nil, err
	}
	return i.a.blob(m.Config.Digest)
}

func (i *rawImage) LayerByDigest(h v1.Hash) (partial.CompressedLayer, error) {
	m, err := i.manifest()
	if err != nil {
		return nil, err
	}
	if h == m.Config.Digest {
		return &blobLayer{a: i.a, d: m.Config}, nil
	}
	for _, d := range m.Layers {
		if d.Digest == h {
			return &blobLayer{a: i.a, d: d}, nil
		}
	}
	return nil, fmt.Errorf("image names no layer %s", h)
}

// blobLayer is a blob of the tarball, described as a manifest describes
// it. A blob the tarball does not hold, such as a layer not meant to be
// distributed, fails only when it is read.
type blobLayer struct {
	a *archive
	d v1.Descriptor
}

func (l *blobLayer) Digest() (v1.Hash, error)            { return l.d.Digest, nil }
func (l *blobLayer) Size() (int64, error)                { return l.d.Size, nil }
func (l *blobLayer) MediaType() (types.MediaType, error) { return l.d.MediaType, nil }

func (l *blobLayer) Compressed() (io.ReadCloser, error) {
	s, ok := l.a.blobs[l.d.Digest]
	if !ok {
		return nil, fmt.Errorf("holds no blob %s", l.d.Digest)
	}
	return io.NopCloser(io.NewSectionReader(s, 0, s.Size())), nil
}

// rawIndex is an image index exactly as the tarball holds it.
type rawIndex struct {
	a         *archive
	raw       []byte
	mediaType types.MediaType
}

func (x *rawIndex) MediaType() (types.MediaType, error) { return x.mediaType, nil }
func (x *rawIndex) RawManifest() ([]byte, error)        { return x.raw, nil }
func (x *rawIndex) Size() (int64, error)                { return int64(len(x.raw)), nil }

func (x *rawIndex) Digest() (v1.Hash, error) {
	h, _, err := v1.SHA256(bytes.NewReader(x.raw))
	return h, err
}

func (x *rawIndex) IndexManifest() (*v1.IndexManifest, error) {
	return v1.ParseIndexManifest(bytes.NewReader(x.raw))
}

func (x *rawIndex) child(h v1.Hash) (v1.Descriptor, error) {
	m, err := x.IndexManifest()
	if err != nil {
		return v1.Descriptor{}, err
	}
	for _, d := range m.Manifests {
		if d.Digest == h {
			return d, nil
		}
	}
	return v1.Descriptor{}, fmt.Errorf("index names no manifest %s", h)
}

func (x *rawIndex) Image(h v1.Hash) (v1.Image, error) {
	d, err := x.child(h)
	if err != nil {
		return nil, err
	}
	if !d.MediaType.IsImage() {
		return nil, fmt.Errorf("%s is a %s, not an image", h, d.MediaType)
	}
	t, err := x.a.manifest(d)
	if err != nil {
		return nil, err
	}
	return t.(v1.Image), nil
}

func (x *rawIndex) ImageIndex(h v1.Hash) (v1.ImageIndex, error) {
	d, err := x.child(h)
	if err != nil {
		return nil, err
	}
	if !d.MediaType.IsIndex() {
		return nil, fmt.Errorf("%s is a %s, not an index", h, d.MediaType)
	}
	t, err := x.a.manifest(d)
	if err != nil {
		return nil, err
	}
	return t.(v1.ImageIndex), nil
}

// Layer is a child that is neither an image nor an index, pushed as the
// blob it is.
func (x *rawIndex) Layer(h v1.Hash) (v1.Layer, error) {
	d, err := x.child(h)
	if err != nil {
		return nil, err
	}
	return partial.CompressedToLayer(&blobLayer{a: x.a, d: d})
}
