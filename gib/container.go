package gib

import (
	"bytes"
	"encoding/json"
	"sort"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// Container represents the result of a successful containerization.
type Container struct {
	// Digest is the image digest (e.g., sha256:...).
	Digest v1.Hash
	// ImageID is the image config digest.
	ImageID v1.Hash
	// Tags are every tag the image was written under, the target's own
	// first.
	Tags []string
	// TargetImage is the target's image reference, as Jib prints it.
	TargetImage string
	// ImagePushed is whether the image was pushed to a registry.
	ImagePushed bool
}

// Metadata is the container as Jib's --image-metadata-out writes it:
// compact JSON with its tags sorted.
func (c *Container) Metadata() ([]byte, error) {
	tags := append([]string{}, c.Tags...)
	sort.Strings(tags)
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	err := e.Encode(struct {
		Image       string   `json:"image"`
		ImageID     string   `json:"imageId"`
		ImageDigest string   `json:"imageDigest"`
		Tags        []string `json:"tags"`
		ImagePushed bool     `json:"imagePushed"`
	}{c.TargetImage, c.ImageID.String(), c.Digest.String(), tags, c.ImagePushed})
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), err
}
