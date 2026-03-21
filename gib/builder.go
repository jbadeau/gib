package gib

import (
	"context"
	"fmt"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/jbadeau/gib/internal/build"
	"github.com/jbadeau/gib/internal/layer"
)

// Platform represents a target platform.
type Platform struct {
	Architecture string
	OS           string
}

// ContainerBuilder configures a container image build.
// Errors are deferred until Containerize is called.
type ContainerBuilder struct {
	source           ImageSource
	layers           []FileEntriesLayer
	entrypoint       []string
	programArguments []string
	environment      map[string]string
	labels           map[string]string
	exposedPorts     []Port
	volumes          []string
	user             string
	workingDirectory string
	creationTime     *int64
	format           ImageFormat
	platforms        []Platform
	progressCallback ProgressCallback
	err              error
}

// AddFileEntriesLayer adds a layer to the image.
func (b *ContainerBuilder) AddFileEntriesLayer(l FileEntriesLayer) *ContainerBuilder {
	b.layers = append(b.layers, l)
	return b
}

// SetEntrypoint sets the container entrypoint.
func (b *ContainerBuilder) SetEntrypoint(args ...string) *ContainerBuilder {
	b.entrypoint = args
	return b
}

// SetProgramArguments sets the container CMD.
func (b *ContainerBuilder) SetProgramArguments(args ...string) *ContainerBuilder {
	b.programArguments = args
	return b
}

// SetEnvironment sets the environment variables.
func (b *ContainerBuilder) SetEnvironment(env map[string]string) *ContainerBuilder {
	b.environment = env
	return b
}

// AddLabel adds a label to the image.
func (b *ContainerBuilder) AddLabel(key, value string) *ContainerBuilder {
	if b.labels == nil {
		b.labels = make(map[string]string)
	}
	b.labels[key] = value
	return b
}

// SetLabels sets the image labels.
func (b *ContainerBuilder) SetLabels(labels map[string]string) *ContainerBuilder {
	b.labels = labels
	return b
}

// SetExposedPorts sets the exposed ports.
func (b *ContainerBuilder) SetExposedPorts(ports ...Port) *ContainerBuilder {
	b.exposedPorts = ports
	return b
}

// AddExposedPort appends an exposed port.
func (b *ContainerBuilder) AddExposedPort(port Port) *ContainerBuilder {
	b.exposedPorts = append(b.exposedPorts, port)
	return b
}

// SetVolumes sets the volumes.
func (b *ContainerBuilder) SetVolumes(volumes ...string) *ContainerBuilder {
	b.volumes = volumes
	return b
}

// AddVolume appends a volume.
func (b *ContainerBuilder) AddVolume(volume string) *ContainerBuilder {
	b.volumes = append(b.volumes, volume)
	return b
}

// SetUser sets the user for the container.
func (b *ContainerBuilder) SetUser(user string) *ContainerBuilder {
	b.user = user
	return b
}

// SetWorkingDirectory sets the working directory.
func (b *ContainerBuilder) SetWorkingDirectory(dir string) *ContainerBuilder {
	b.workingDirectory = dir
	return b
}

// SetCreationTime sets the image creation time in milliseconds since epoch.
func (b *ContainerBuilder) SetCreationTime(millis int64) *ContainerBuilder {
	b.creationTime = &millis
	return b
}

// SetFormat sets the image format (Docker or OCI).
func (b *ContainerBuilder) SetFormat(format ImageFormat) *ContainerBuilder {
	b.format = format
	return b
}

// AddPlatform adds a target platform.
func (b *ContainerBuilder) AddPlatform(architecture, os string) *ContainerBuilder {
	b.platforms = append(b.platforms, Platform{Architecture: architecture, OS: os})
	return b
}

// GetPlatforms returns the configured target platforms.
func (b *ContainerBuilder) GetPlatforms() []Platform {
	return b.platforms
}

// OnProgress sets a callback that receives build progress events.
func (b *ContainerBuilder) OnProgress(cb ProgressCallback) *ContainerBuilder {
	b.progressCallback = cb
	return b
}

func (b *ContainerBuilder) emitProgress(phase ProgressPhase, message string) {
	if b.progressCallback != nil {
		b.progressCallback(ProgressEvent{Phase: phase, Message: message})
	}
}

// Containerize builds the image and writes it to the given target.
func (b *ContainerBuilder) Containerize(ctx context.Context, target *Containerizer) (*Container, error) {
	if b.err != nil {
		return nil, b.err
	}

	b.emitProgress(PhaseContainerizing, fmt.Sprintf("Containerizing application to %s...", target.Description()))

	b.emitProgress(PhasePullingBase, fmt.Sprintf("Pulling base image %s...", b.source.description()))

	baseImage, err := b.source.resolve(ctx)
	if err != nil {
		return nil, &BuildError{Message: "failed to resolve base image", Cause: err}
	}

	// Build reproducible layers
	var v1Layers []v1.Layer
	for _, fel := range b.layers {
		b.emitProgress(PhaseBuildingLayer, fmt.Sprintf("Building layer %s...", fel.Name))
		// Convert FileEntry -> layer.Entry
		entries := make([]layer.Entry, len(fel.Entries))
		for i, e := range fel.Entries {
			entries[i] = layer.Entry{
				SourcePath:       e.SourcePath,
				DestinationPath:  e.DestinationPath,
				Permissions:      e.Permissions,
				ModificationTime: e.ModificationTime,
				Ownership:        e.Ownership,
			}
		}
		l, err := layer.BuildReproducibleLayer(entries)
		if err != nil {
			return nil, &BuildError{Message: fmt.Sprintf("building layer %q", fel.Name), Cause: err}
		}
		v1Layers = append(v1Layers, l)
	}

	// Convert ports to strings
	var portStrs []string
	for _, p := range b.exposedPorts {
		portStrs = append(portStrs, p.String())
	}

	// Determine media type
	var mediaType types.MediaType
	switch b.format {
	case OCIFormat:
		mediaType = types.OCIManifestSchema1
	default:
		mediaType = types.DockerManifestSchema2
	}

	req := build.Request{
		BaseImage:        baseImage,
		Layers:           v1Layers,
		Entrypoint:       b.entrypoint,
		ProgramArguments: b.programArguments,
		Environment:      b.environment,
		Labels:           b.labels,
		ExposedPorts:     portStrs,
		Volumes:          b.volumes,
		User:             b.user,
		WorkingDirectory: b.workingDirectory,
		CreationTimeMs:   b.creationTime,
		MediaType:        mediaType,
	}

	b.emitProgress(PhaseBuildingImage, "Building image...")

	image, err := build.Execute(ctx, req)
	if err != nil {
		return nil, &BuildError{Message: "build failed", Cause: err}
	}

	b.emitProgress(PhaseWriting, fmt.Sprintf("Writing to %s...", target.Description()))

	result, err := target.write(ctx, image)
	if err != nil {
		return nil, err
	}

	b.emitProgress(PhaseFinalizing, "Finalizing...")

	return result, nil
}
