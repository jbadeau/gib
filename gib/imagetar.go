package gib

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// writeImageTar writes image to path as a tarball that is both an OCI
// image layout and a docker-save archive, the way Docker's own save
// writes one: every blob once under blobs/<algorithm>/<hex>, the image's
// manifest among them with its exact bytes, index.json naming it and
// manifest.json naming its config and layers. `docker load` reads it,
// and `gib push` pushes the very manifest built here, so the digest a
// registry reports is the one the build reports, whatever the format.
func writeImageTar(file string, tag name.Tag, img v1.Image) (err error) {
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return writeImage(f, tag, img)
}

func writeImage(w io.Writer, tag name.Tag, img v1.Image) error {
	raw, err := img.RawManifest()
	if err != nil {
		return err
	}
	m, err := img.Manifest()
	if err != nil {
		return err
	}
	digest, err := img.Digest()
	if err != nil {
		return err
	}
	mt, err := img.MediaType()
	if err != nil {
		return err
	}
	cfg, err := img.RawConfigFile()
	if err != nil {
		return err
	}

	tw := tar.NewWriter(w)
	written := map[v1.Hash]bool{}
	blob := func(h v1.Hash, open func() (io.ReadCloser, error), size int64) error {
		if written[h] {
			return nil
		}
		written[h] = true
		rc, err := open()
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		if err := tw.WriteHeader(header(blobPath(h), size)); err != nil {
			return err
		}
		n, err := io.Copy(tw, rc)
		if err != nil {
			return err
		}
		if n != size {
			return fmt.Errorf("blob %s is %d bytes, its descriptor says %d", h, n, size)
		}
		return nil
	}
	bytesOf := func(b []byte) func() (io.ReadCloser, error) {
		return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }
	}

	if err := blob(m.Config.Digest, bytesOf(cfg), int64(len(cfg))); err != nil {
		return err
	}
	layers, err := img.Layers()
	if err != nil {
		return err
	}
	var layerPaths []string
	for i, l := range layers {
		d := m.Layers[i]
		layerPaths = append(layerPaths, blobPath(d.Digest))
		if !d.MediaType.IsDistributable() {
			continue
		}
		if err := blob(d.Digest, l.Compressed, d.Size); err != nil {
			return fmt.Errorf("layer %s: %w", d.Digest, err)
		}
	}
	if err := blob(digest, bytesOf(raw), int64(len(raw))); err != nil {
		return err
	}

	index, err := json.Marshal(v1.IndexManifest{
		SchemaVersion: 2,
		MediaType:     types.OCIImageIndex,
		Manifests: []v1.Descriptor{{
			MediaType: mt,
			Size:      int64(len(raw)),
			Digest:    digest,
			Annotations: map[string]string{
				"io.containerd.image.name":          tag.String(),
				"org.opencontainers.image.ref.name": tag.TagStr(),
			},
		}},
	})
	if err != nil {
		return err
	}
	docker, err := json.Marshal([]dockerImage{{
		Config:   blobPath(m.Config.Digest),
		RepoTags: []string{tag.String()},
		Layers:   layerPaths,
	}})
	if err != nil {
		return err
	}
	for _, f := range []struct {
		name string
		data []byte
	}{
		{"index.json", index},
		{"manifest.json", docker},
		{"oci-layout", []byte(`{"imageLayoutVersion":"1.0.0"}`)},
	} {
		if err := tw.WriteHeader(header(f.name, int64(len(f.data)))); err != nil {
			return err
		}
		if _, err := tw.Write(f.data); err != nil {
			return err
		}
	}
	return tw.Close()
}

// dockerImage is one entry of a docker-save archive's manifest.json.
type dockerImage struct {
	Config   string
	RepoTags []string
	Layers   []string
}

func blobPath(h v1.Hash) string { return path.Join("blobs", h.Algorithm, h.Hex) }

// header is a regular file's, with nothing in it that varies between
// builds of the same image.
func header(name string, size int64) *tar.Header {
	return &tar.Header{Name: name, Size: size, Mode: 0o644, Typeflag: tar.TypeReg}
}
