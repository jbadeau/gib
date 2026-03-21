// Package gib provides a daemonless container image builder.
//
// gib builds container images without requiring a Docker daemon,
// compatible with jib's build file format (jib.yaml).
package gib

// From creates a new ContainerBuilder from a registry image reference.
func From(imageRef string, opts ...ImageSourceOption) *ContainerBuilder {
	return &ContainerBuilder{
		source: RegistrySource(imageRef, opts...),
	}
}

// FromImage creates a new ContainerBuilder from an explicit ImageSource.
func FromImage(source ImageSource) *ContainerBuilder {
	return &ContainerBuilder{
		source: source,
	}
}

// FromScratch creates a new ContainerBuilder from an empty base image.
func FromScratch() *ContainerBuilder {
	return &ContainerBuilder{
		source: &scratchSource{},
	}
}
