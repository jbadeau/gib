package gib

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// writeDocker loads the image into the Docker daemon as Jib's
// CliDockerClient does: the daemon asked for its platform with `docker
// info`, then the image tarball piped into `docker load`. Of several
// platforms' images, the daemon gets the one for its platform, or the
// first.
func (c *Containerizer) writeDocker(ctx context.Context, ref reference, tags []string, images []v1.Image) (*Container, error) {
	info, err := c.dockerInfo(ctx)
	if err != nil {
		return nil, err
	}
	img := images[0]
	if len(images) > 1 {
		arch := info.Architecture
		switch arch {
		case "x86_64":
			arch = "amd64"
		case "aarch64":
			arch = "arm64"
		}
		c.settings.log.log(LevelWarn, "Detected multi-platform configuration, only building image that matches the local Docker Engine's os and architecture (%s/%s) or the first platform specified", info.OSType, arch)
		for _, i := range images {
			cfg, err := i.ConfigFile()
			if err != nil {
				return nil, err
			}
			if cfg.Architecture == arch && cfg.OS == info.OSType {
				img = i
				break
			}
		}
	}

	c.settings.log.log(LevelProgress, "Loading to Docker daemon...")
	cmd := exec.CommandContext(ctx, c.docker, "load")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	werr := writeImage(stdin, repoTags(ref, tags), ref.withQualifierString(), img)
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("'docker load' command failed with error: %s", strings.TrimSpace(stderr.String()))
	}
	if werr != nil {
		return nil, fmt.Errorf("'docker load' command failed with error: %w", werr)
	}
	c.settings.log.log(LevelDebug, "%s", stdout.String())
	return local(ref, tags, img)
}

type dockerInfo struct {
	OSType       string `json:"OSType"`
	Architecture string `json:"Architecture"`
}

func (c *Containerizer) dockerInfo(ctx context.Context) (dockerInfo, error) {
	var info dockerInfo
	cmd := exec.CommandContext(ctx, c.docker, "info", "-f", "{{json .}}")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return info, fmt.Errorf("'docker info' command failed with error: %s", strings.TrimSpace(stderr.String()))
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return info, fmt.Errorf("Failed to read output of 'docker info': %w", err) //nolint:staticcheck // Jib's message
	}
	return info, nil
}
