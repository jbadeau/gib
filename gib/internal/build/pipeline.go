package build

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// Request holds all configuration for a build.
// All types are from go-containerregistry or stdlib to avoid import cycles.
type Request struct {
	BaseImage        v1.Image
	Layers           []v1.Layer
	Entrypoint       []string
	ProgramArguments []string
	Environment      map[string]string
	Labels           map[string]string
	ExposedPorts     []string // "port/protocol" format
	Volumes          []string
	User             string
	WorkingDirectory string
	CreationTimeMs   *int64 // millis since epoch
	MediaType        types.MediaType
}

// Execute runs the build pipeline: layers -> config -> format.
func Execute(_ context.Context, req Request) (v1.Image, error) {
	image := req.BaseImage

	// 1. Append layers
	if len(req.Layers) > 0 {
		var err error
		image, err = mutate.AppendLayers(image, req.Layers...)
		if err != nil {
			return nil, fmt.Errorf("appending layers: %w", err)
		}
	}

	// 2. Apply config
	cfg, err := image.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	cfg = cfg.DeepCopy()

	if req.Entrypoint != nil {
		cfg.Config.Entrypoint = req.Entrypoint
	}
	if req.ProgramArguments != nil {
		cfg.Config.Cmd = req.ProgramArguments
	}
	if req.User != "" {
		cfg.Config.User = req.User
	}
	if req.WorkingDirectory != "" {
		cfg.Config.WorkingDir = req.WorkingDirectory
	}

	// Merge environment (append/overwrite)
	if len(req.Environment) > 0 {
		envMap := make(map[string]string)
		for _, e := range cfg.Config.Env {
			parts := splitEnv(e)
			envMap[parts[0]] = parts[1]
		}
		for k, v := range req.Environment {
			envMap[k] = v
		}
		var envSlice []string
		for k, v := range envMap {
			envSlice = append(envSlice, k+"="+v)
		}
		cfg.Config.Env = envSlice
	}

	// Merge labels (overwrite on collision)
	if len(req.Labels) > 0 {
		if cfg.Config.Labels == nil {
			cfg.Config.Labels = make(map[string]string)
		}
		for k, v := range req.Labels {
			cfg.Config.Labels[k] = v
		}
	}

	// Append exposed ports
	if len(req.ExposedPorts) > 0 {
		if cfg.Config.ExposedPorts == nil {
			cfg.Config.ExposedPorts = make(map[string]struct{})
		}
		for _, p := range req.ExposedPorts {
			cfg.Config.ExposedPorts[p] = struct{}{}
		}
	}

	// Append volumes
	if len(req.Volumes) > 0 {
		if cfg.Config.Volumes == nil {
			cfg.Config.Volumes = make(map[string]struct{})
		}
		for _, v := range req.Volumes {
			cfg.Config.Volumes[v] = struct{}{}
		}
	}

	// Set creation time
	if req.CreationTimeMs != nil {
		ms := *req.CreationTimeMs
		t := time.Unix(ms/1000, (ms%1000)*int64(time.Millisecond)).UTC()
		cfg.Created = v1.Time{Time: t}
	}

	image, err = mutate.ConfigFile(image, cfg)
	if err != nil {
		return nil, fmt.Errorf("applying config: %w", err)
	}

	// 3. Set media type / format
	if req.MediaType != "" {
		image = mutate.MediaType(image, req.MediaType)
	}

	return image, nil
}

func splitEnv(env string) [2]string {
	for i, c := range env {
		if c == '=' {
			return [2]string{env[:i], env[i+1:]}
		}
	}
	return [2]string{env, ""}
}
