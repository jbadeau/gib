package gib

import "fmt"

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

// ParseImageFormat parses a format as Jib's ImageFormat.valueOf does:
// "Docker" or "OCI", exactly.
func ParseImageFormat(s string) (ImageFormat, error) {
	switch s {
	case "Docker":
		return DockerFormat, nil
	case "OCI":
		return OCIFormat, nil
	}
	return 0, fmt.Errorf("No enum constant com.google.cloud.tools.jib.api.buildplan.ImageFormat.%s", s) //nolint:staticcheck // Java's message
}
