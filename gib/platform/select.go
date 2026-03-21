package platform

import v1 "github.com/google/go-containerregistry/pkg/v1"

// Spec represents a target platform.
type Spec struct {
	Architecture string
	OS           string
}

// ToV1Platform converts to a go-containerregistry Platform.
func (s Spec) ToV1Platform() v1.Platform {
	return v1.Platform{
		Architecture: s.Architecture,
		OS:           s.OS,
	}
}
