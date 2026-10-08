package gib

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/moby/moby/client"
)

// writeDocker loads the image into the Docker daemon under every tag, as
// Jib's CliDockerClient does. Of several platforms' images, the daemon
// gets the one for its own platform, or the first.
func (c *Containerizer) writeDocker(ctx context.Context, ref reference, tags []string, images []v1.Image) (*Container, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cli.Close() }()
	img := images[0]
	if len(images) > 1 {
		info, err := cli.Info(ctx, client.InfoOptions{})
		if err != nil {
			return nil, err
		}
		arch := info.Info.Architecture
		switch arch {
		case "x86_64":
			arch = "amd64"
		case "aarch64":
			arch = "arm64"
		}
		c.settings.log.log(LevelWarn, "Detected multi-platform configuration, only building image that matches the local Docker Engine's os and architecture (%s/%s) or the first platform specified", info.Info.OSType, arch)
		for _, i := range images {
			cfg, err := i.ConfigFile()
			if err != nil {
				return nil, err
			}
			if cfg.Architecture == arch && cfg.OS == info.Info.OSType {
				img = i
				break
			}
		}
	}

	c.settings.log.log(LevelProgress, "Loading to Docker daemon...")
	opts := []daemon.Option{daemon.WithClient(cli), daemon.WithContext(ctx)}
	var first name.Tag
	for i, t := range tags {
		tag, err := dockerTag(ref, t)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			first = tag
			out, err := daemon.Write(tag, img, opts...)
			if err != nil {
				return nil, fmt.Errorf("loading to Docker daemon: %w", err)
			}
			c.settings.log.log(LevelDebug, "%s", out)
			continue
		}
		if err := daemon.Tag(first, tag, opts...); err != nil {
			return nil, fmt.Errorf("tagging %s: %w", tag, err)
		}
	}
	return local(ref, tags, img)
}

// dockerTag is ref under tag as the daemon names it. A digest is no
// name the daemon takes, so a digest qualifier names the image latest.
func dockerTag(ref reference, tag string) (name.Tag, error) {
	repo, err := ref.repoName(false)
	if err != nil {
		return name.Tag{}, err
	}
	if digestRE.MatchString(tag) {
		tag = defaultTag
	}
	return repo.Tag(tag), nil
}
