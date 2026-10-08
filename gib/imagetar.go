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

// writeImageTar writes image to path as Jib writes an image tarball:
// a Docker-format image as a docker-save archive, its layers as
// <hex>.tar.gz beside config.json and manifest.json, and an OCI-format
// image as an OCI image layout, every blob under blobs/sha256/ with the
// manifest's exact bytes among them and index.json naming it. `docker
// load` reads either, and `gib push` pushes the manifest built here, so
// the digest a registry reports is the one the build reports.
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
	layers, err := img.Layers()
	if err != nil {
		return err
	}
	oci := mt == types.OCIManifestSchema1

	tw := tar.NewWriter(w)
	file := func(name string, size int64, open func() (io.ReadCloser, error)) error {
		rc, err := open()
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		if err := tw.WriteHeader(header(name, size)); err != nil {
			return err
		}
		n, err := io.Copy(tw, rc)
		if err != nil {
			return err
		}
		if n != size {
			return fmt.Errorf("%s is %d bytes, its descriptor says %d", name, n, size)
		}
		return nil
	}
	data := func(name string, b []byte) error {
		return file(name, int64(len(b)), func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil })
	}

	written := map[string]bool{}
	var layerPaths []string
	for i, l := range layers {
		d := m.Layers[i]
		p := d.Digest.Hex + ".tar.gz"
		if oci {
			p = blobPath(d.Digest)
		}
		layerPaths = append(layerPaths, p)
		if !d.MediaType.IsDistributable() || written[p] {
			continue
		}
		written[p] = true
		if err := file(p, d.Size, l.Compressed); err != nil {
			return fmt.Errorf("layer %s: %w", d.Digest, err)
		}
	}

	if !oci {
		docker, err := json.Marshal([]dockerImage{{Config: "config.json", RepoTags: []string{tag.String()}, Layers: layerPaths}})
		if err != nil {
			return err
		}
		if err := data("config.json", cfg); err != nil {
			return err
		}
		if err := data("manifest.json", docker); err != nil {
			return err
		}
		return tw.Close()
	}

	if err := data(blobPath(m.Config.Digest), cfg); err != nil {
		return err
	}
	if err := data(blobPath(digest), raw); err != nil {
		return err
	}
	index, err := json.Marshal(v1.IndexManifest{
		SchemaVersion: 2,
		MediaType:     types.OCIImageIndex,
		Manifests: []v1.Descriptor{{
			MediaType:   mt,
			Size:        int64(len(raw)),
			Digest:      digest,
			Annotations: map[string]string{"org.opencontainers.image.ref.name": tag.String()},
		}},
	})
	if err != nil {
		return err
	}
	if err := data("oci-layout", []byte(`{"imageLayoutVersion": "1.0.0"}`)); err != nil {
		return err
	}
	if err := data("index.json", index); err != nil {
		return err
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
