package gib

import v1 "github.com/google/go-containerregistry/pkg/v1"

// Container represents the result of a successful containerization.
type Container struct {
	// Digest is the image digest (e.g., sha256:...).
	Digest v1.Hash
	// ImageID is the image config digest.
	ImageID v1.Hash
	// Tags are the tags that were applied to the image.
	Tags []string
	// TargetImage is the image reference that was pushed.
	TargetImage string
}
