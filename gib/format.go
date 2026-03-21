package gib

// ImageFormat represents the output image format.
type ImageFormat int

const (
	// DockerFormat produces a Docker V2.2 manifest.
	DockerFormat ImageFormat = iota
	// OCIFormat produces an OCI image manifest.
	OCIFormat
)

// String returns the string representation of the format.
func (f ImageFormat) String() string {
	switch f {
	case OCIFormat:
		return "OCI"
	default:
		return "Docker"
	}
}

// ParseImageFormat parses a string into an ImageFormat.
func ParseImageFormat(s string) ImageFormat {
	switch s {
	case "OCI":
		return OCIFormat
	default:
		return DockerFormat
	}
}
