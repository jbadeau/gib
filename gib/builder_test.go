package gib

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainerBuilder_SetEntrypoint(t *testing.T) {
	b := FromScratch().SetEntrypoint("sh", "run.sh")
	assert.Equal(t, []string{"sh", "run.sh"}, b.entrypoint)
}

func TestContainerBuilder_SetProgramArguments(t *testing.T) {
	b := FromScratch().SetProgramArguments("--flag", "value")
	assert.Equal(t, []string{"--flag", "value"}, b.programArguments)
}

func TestContainerBuilder_SetEnvironment(t *testing.T) {
	env := map[string]string{"KEY": "VAL"}
	b := FromScratch().SetEnvironment(env)
	assert.Equal(t, env, b.environment)
}

func TestContainerBuilder_AddLabel(t *testing.T) {
	b := FromScratch().
		AddLabel("k1", "v1").
		AddLabel("k2", "v2")
	assert.Equal(t, "v1", b.labels["k1"])
	assert.Equal(t, "v2", b.labels["k2"])
}

func TestContainerBuilder_SetLabels(t *testing.T) {
	labels := map[string]string{"k": "v"}
	b := FromScratch().SetLabels(labels)
	assert.Equal(t, labels, b.labels)
}

func TestContainerBuilder_SetExposedPorts(t *testing.T) {
	p := Port{Number: 8080, Protocol: "tcp"}
	b := FromScratch().SetExposedPorts(p)
	assert.Equal(t, []Port{p}, b.exposedPorts)
}

func TestContainerBuilder_AddExposedPort(t *testing.T) {
	p1 := Port{Number: 8080, Protocol: "tcp"}
	p2 := Port{Number: 9090, Protocol: "udp"}
	b := FromScratch().AddExposedPort(p1).AddExposedPort(p2)
	assert.Equal(t, []Port{p1, p2}, b.exposedPorts)
}

func TestContainerBuilder_SetVolumes(t *testing.T) {
	b := FromScratch().SetVolumes("/vol1", "/vol2")
	assert.Equal(t, []string{"/vol1", "/vol2"}, b.volumes)
}

func TestContainerBuilder_AddVolume(t *testing.T) {
	b := FromScratch().AddVolume("/vol1").AddVolume("/vol2")
	assert.Equal(t, []string{"/vol1", "/vol2"}, b.volumes)
}

func TestContainerBuilder_SetUser(t *testing.T) {
	b := FromScratch().SetUser("appuser")
	assert.Equal(t, "appuser", b.user)
}

func TestContainerBuilder_SetWorkingDirectory(t *testing.T) {
	b := FromScratch().SetWorkingDirectory("/app")
	assert.Equal(t, "/app", b.workingDirectory)
}

func TestContainerBuilder_SetCreationTime(t *testing.T) {
	b := FromScratch().SetCreationTime(2000)
	assert.NotNil(t, b.creationTime)
	assert.Equal(t, int64(2000), *b.creationTime)
}

func TestContainerBuilder_SetFormat(t *testing.T) {
	b := FromScratch().SetFormat(OCIFormat)
	assert.Equal(t, OCIFormat, b.format)
}

func TestContainerBuilder_AddPlatform(t *testing.T) {
	b := FromScratch().
		AddPlatform("amd64", "linux").
		AddPlatform("arm64", "linux")
	assert.Len(t, b.platforms, 2)
	assert.Equal(t, "amd64", b.platforms[0].Architecture)
	assert.Equal(t, "linux", b.platforms[0].OS)
}

func TestContainerBuilder_AddFileEntriesLayer(t *testing.T) {
	l := FileEntriesLayer{Name: "test"}
	b := FromScratch().AddFileEntriesLayer(l)
	assert.Len(t, b.layers, 1)
	assert.Equal(t, "test", b.layers[0].Name)
}

func TestContainerBuilder_MethodChaining(t *testing.T) {
	b := FromScratch().
		SetEntrypoint("sh").
		SetProgramArguments("--flag").
		SetUser("user").
		SetWorkingDirectory("/app").
		SetFormat(OCIFormat).
		AddPlatform("amd64", "linux").
		AddLabel("k", "v").
		AddVolume("/data").
		AddExposedPort(Port{Number: 80, Protocol: "tcp"})

	assert.Equal(t, []string{"sh"}, b.entrypoint)
	assert.Equal(t, []string{"--flag"}, b.programArguments)
	assert.Equal(t, "user", b.user)
	assert.Equal(t, "/app", b.workingDirectory)
	assert.Equal(t, OCIFormat, b.format)
	assert.Len(t, b.platforms, 1)
	assert.Equal(t, "v", b.labels["k"])
	assert.Equal(t, []string{"/data"}, b.volumes)
	assert.Len(t, b.exposedPorts, 1)
}

func TestContainerBuilder_Platform(t *testing.T) {
	tests := []struct {
		name      string
		platforms [][2]string
		want      string
		err       string
	}{
		{name: "none is Jib's default", want: "linux/amd64"},
		{name: "the one named", platforms: [][2]string{{"arm64", "linux"}}, want: "linux/arm64"},
		{name: "several are refused", platforms: [][2]string{{"arm64", "linux"}, {"amd64", "linux"}}, err: "2 platforms are named"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := FromScratch().SetEntrypoint("/app")
			for _, p := range tt.platforms {
				b.AddPlatform(p[0], p[1])
			}
			tarPath := filepath.Join(t.TempDir(), "image.tar")
			_, err := b.Containerize(context.Background(), ToTar(tarPath, WithTarImageName("app")))
			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			image, err := tarball.ImageFromPath(tarPath, nil)
			require.NoError(t, err)
			cfg, err := image.ConfigFile()
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.OS+"/"+cfg.Architecture)
		})
	}
}
