package gib

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// logs collects what a build logs.
type logs struct {
	mu     sync.Mutex
	events []LogEvent
}

func (l *logs) handle(e LogEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *logs) at(level LogLevel) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, e := range l.events {
		if e.Level == level {
			out = append(out, e.Message)
		}
	}
	return out
}

// serveWithAuth starts an in-process registry that asks for user and
// password, as a Basic challenge, and returns its host.
func serveWithAuth(t *testing.T, user, password string) string {
	t.Helper()
	reg := registry.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		reg.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

// isolated keeps a test from the credentials of whoever runs it.
func isolated(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, v := range []string{"XDG_RUNTIME_DIR", "XDG_CONFIG_HOME", "DOCKER_CONFIG"} {
		if old, ok := os.LookupEnv(v); ok {
			require.NoError(t, os.Unsetenv(v))
			t.Cleanup(func() { _ = os.Setenv(v, old) })
		}
	}
	return home
}

func scratch(t *testing.T) *ContainerBuilder {
	t.Helper()
	src := filepath.Join(t.TempDir(), "hello.txt")
	require.NoError(t, os.WriteFile(src, []byte("hello"), 0o644))
	l, err := NewFileEntriesLayerBuilder().SetName("app").AddEntry(src, "/app/hello.txt").Build()
	require.NoError(t, err)
	return FromScratch().AddFileEntriesLayer(l)
}

func TestRegistry_PlainHTTPIsRefusedUnlessInsecureRegistriesAreAllowed(t *testing.T) {
	isolated(t)
	host := serve(t)

	_, err := scratch(t).Containerize(context.Background(), ToRegistry(host+"/acme/app"))

	var insecure *InsecureRegistryError
	require.ErrorAs(t, err, &insecure)
	assert.Equal(t, "Failed to verify the server at https://"+host+"/v2/ because only secure connections are allowed.", insecure.Error())
}

func TestRegistry_InsecureRegistriesFallBackToHTTP(t *testing.T) {
	isolated(t)
	host := serve(t)

	c, err := scratch(t).Containerize(context.Background(),
		ToRegistry("registry://"+host+"/acme/app", WithAllowInsecureRegistries(true)))

	require.NoError(t, err)
	assert.Equal(t, host+"/acme/app", c.TargetImage)
}

func TestRegistry_InsecureRegistriesSkipVerifyingACertificate(t *testing.T) {
	isolated(t)
	srv := httptest.NewTLSServer(registry.New())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "https://")

	_, err := scratch(t).Containerize(context.Background(), ToRegistry(host+"/acme/app"))
	var insecure *InsecureRegistryError
	require.ErrorAs(t, err, &insecure)

	_, err = scratch(t).Containerize(context.Background(),
		ToRegistry(host+"/acme/app", WithAllowInsecureRegistries(true)))
	require.NoError(t, err)
}

func TestRegistry_CredentialsAreNotSentOverHTTP(t *testing.T) {
	isolated(t)
	host := serveWithAuth(t, "u", "p")

	_, err := scratch(t).Containerize(context.Background(), ToRegistry(host+"/acme/app",
		WithAllowInsecureRegistries(true), WithCredential(Credential{"u", "p"}, "--username/--password")))

	var notSent *CredentialsNotSentError
	require.ErrorAs(t, err, &notSent)
	assert.Equal(t, "Required credentials for "+host+"/acme/app were not sent because the connection was over HTTP", notSent.Error())
}

func TestRegistry_CredentialsAreSentOverHTTPWhenAllowed(t *testing.T) {
	isolated(t)
	host := serveWithAuth(t, "u", "p")
	l := &logs{}

	_, err := scratch(t).OnLog(l.handle).Containerize(context.Background(), ToRegistry(host+"/acme/app",
		WithAllowInsecureRegistries(true), WithSendCredentialsOverHTTP(true),
		WithCredential(Credential{"u", "p"}, "--username/--password")))

	require.NoError(t, err)
	assert.Contains(t, l.at(LevelLifecycle), "Using credentials from --username/--password for "+host+"/acme/app")
}

func TestRegistry_ReportsEveryTagItPushedUnder(t *testing.T) {
	isolated(t)
	host := serve(t)

	c, err := scratch(t).Containerize(context.Background(), ToRegistry(host+"/acme/app:1",
		WithAllowInsecureRegistries(true), WithAdditionalTag("a"), WithAdditionalTag("1"), WithAdditionalTag("b")))

	require.NoError(t, err)
	assert.Equal(t, []string{"1", "a", "b"}, c.Tags)
	assert.True(t, c.ImagePushed)
	assert.Equal(t, host+"/acme/app:1", c.TargetImage)
	for _, tag := range c.Tags {
		r, err := name.ParseReference(host + "/acme/app:" + tag)
		require.NoError(t, err)
		d, err := remote.Head(r, remote.WithAuth(authn.Anonymous))
		require.NoError(t, err)
		assert.Equal(t, c.Digest, d.Digest)
	}
}

func TestTar_NamesTheImageByEveryTag(t *testing.T) {
	file := filepath.Join(t.TempDir(), "image.tar")

	c, err := scratch(t).Containerize(context.Background(), ToTar(file, WithTarImageName("test/x"), WithAdditionalTag("a")))

	require.NoError(t, err)
	_, data := entries(t, file)
	var m []dockerImage
	require.NoError(t, json.Unmarshal(data["manifest.json"], &m))
	assert.Equal(t, []string{"test/x:latest", "test/x:a"}, m[0].RepoTags)
	assert.Equal(t, []string{"latest", "a"}, c.Tags)
	assert.Equal(t, "test/x", c.TargetImage)
	assert.False(t, c.ImagePushed)
}

func TestMetadata_IsJibsImageMetadataOutput(t *testing.T) {
	c := &Container{
		TargetImage: "test/x",
		ImageID:     v1.Hash{Algorithm: "sha256", Hex: strings.Repeat("1", 64)},
		Digest:      v1.Hash{Algorithm: "sha256", Hex: strings.Repeat("2", 64)},
		Tags:        []string{"latest", "b", "a"},
	}

	b, err := c.Metadata()

	require.NoError(t, err)
	assert.Equal(t, `{"image":"test/x","imageId":"sha256:`+strings.Repeat("1", 64)+`","imageDigest":"sha256:`+strings.Repeat("2", 64)+`","tags":["a","b","latest"],"imagePushed":false}`, string(b))
}

func TestValidate_RefusesWhatJibRefusesBeforeBuilding(t *testing.T) {
	for _, tc := range []struct {
		c    *Containerizer
		want string
	}{
		{ToRegistry("BAD"), "Invalid image reference: BAD"},
		{ToTar("x.tar", WithTarImageName("Bad Name")), "Invalid image reference: Bad Name"},
		{ToRegistry("gcr.io/p/i", WithCredentialHelper("./nope/helper")), "Specified credential helper was not found: ./nope/helper"},
		{ToRegistry("gcr.io/p/i", WithAdditionalTag("bad tag")), "invalid tag 'bad tag'"},
	} {
		assert.EqualError(t, tc.c.Validate(), tc.want)
	}
	assert.NoError(t, ToTar("x.tar", WithTarImageName("x"), WithCredentialHelper("./nope/helper")).Validate(),
		"a tarball needs no credentials")
}

// daemon skips a test without a Docker daemon to load images into.
func dockerDaemon(t *testing.T) {
	t.Helper()
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("no Docker daemon")
	}
}

func TestDocker_TheDaemonLoadsTheImageUnderEveryTag(t *testing.T) {
	dockerDaemon(t)
	t.Cleanup(func() { _ = exec.Command("docker", "rmi", "gib-test/loaded:1", "gib-test/loaded:a").Run() })

	c, err := scratch(t).SetEntrypoint("/app").Containerize(context.Background(),
		ToDocker("gib-test/loaded:1", WithAdditionalTag("a")))

	require.NoError(t, err)
	var ids []string
	for _, tag := range []string{"gib-test/loaded:1", "gib-test/loaded:a"} {
		out, err := exec.Command("docker", "image", "inspect", "-f", "{{.Id}}", tag).Output()
		require.NoError(t, err, tag)
		ids = append(ids, strings.TrimSpace(string(out)))
	}
	assert.Equal(t, ids[0], ids[1], "one image under both tags")
	assert.Equal(t, "gib-test/loaded:1", c.TargetImage)
	assert.Equal(t, []string{"1", "a"}, c.Tags)
	assert.False(t, c.ImagePushed)
}

func TestDocker_TheDaemonGetsTheImageForItsPlatform(t *testing.T) {
	dockerDaemon(t)
	t.Cleanup(func() { _ = exec.Command("docker", "rmi", "gib-test/platform").Run() })
	out, err := exec.Command("docker", "info", "-f", "{{.Architecture}}").Output()
	require.NoError(t, err)
	arch := map[string]string{"x86_64": "amd64", "aarch64": "arm64"}[strings.TrimSpace(string(out))]

	_, err = scratch(t).AddPlatform("arm64", "linux").AddPlatform("amd64", "linux").SetEntrypoint("/app").
		Containerize(context.Background(), ToDocker("gib-test/platform"))

	require.NoError(t, err)
	got, err := exec.Command("docker", "image", "inspect", "-f", "{{.Architecture}}", "gib-test/platform").Output()
	require.NoError(t, err)
	assert.Equal(t, arch, strings.TrimSpace(string(got)))
}

func TestLog_ABuildLogsAsJibDoes(t *testing.T) {
	l := &logs{}
	file := filepath.Join(t.TempDir(), "image.tar")

	_, err := scratch(t).SetEntrypoint("/app/hello.txt").OnLog(l.handle).
		Containerize(context.Background(), ToTar(file, WithTarImageName("x")))

	require.NoError(t, err)
	var plain []string
	for _, e := range l.events {
		if e.Level == LevelProgress || e.Level == LevelLifecycle {
			plain = append(plain, e.Message)
		}
	}
	assert.Equal(t, []string{
		"Getting scratch base image...",
		"Building app layer...",
		"",
		"Container entrypoint set to [/app/hello.txt]",
		"Building image to tar file...",
	}, plain)
	assert.Equal(t, "Containerizing application with the following files:", l.at(LevelInfo)[0])
	assert.Equal(t, "\tApp:", l.at(LevelInfo)[1])
}
