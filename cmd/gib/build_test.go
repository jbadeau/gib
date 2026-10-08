package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runGib runs gib with args in dir, stdin given, and returns its exit code,
// stdout and stderr.
func runGib(t *testing.T, dir, stdin string, args ...string) (int, string, string) {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(wd) })
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

// project is a context directory holding a build file and app/a.txt.
func project(t *testing.T, buildFile string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "p", "app"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "p", "app", "a.txt"), []byte("hi"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "p", "jib.yaml"), []byte(buildFile), 0o644))
	return dir
}

const appBuildFile = `apiVersion: jib/v1alpha1
kind: BuildFile
layers:
  entries:
    - name: app
      files:
        - src: app
          dest: /app
`

// tarFile is a file of a tarball.
func tarFile(t *testing.T, file, name string) []byte {
	t.Helper()
	f, err := os.Open(file)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	r := tar.NewReader(f)
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			t.Fatalf("%s holds no %s", file, name)
		}
		require.NoError(t, err)
		if h.Name == name {
			b, err := io.ReadAll(r)
			require.NoError(t, err)
			return b
		}
	}
}

// Jib's picocli refuses these command lines with exit code 2, before it
// builds anything.
func TestBuild_RefusesTheCommandLinesJibRefuses(t *testing.T) {
	dir := project(t, appBuildFile)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"build"}, "Missing required option: '--target=<target-image>'"},
		{[]string{"build", "-t"}, "Missing required parameter for option '--target' (<target-image>)"},
		{[]string{"build", "-t", "tar://o.tar", "-c", "p"}, "Missing option: --name must be specified when using --target=tar://...."},
		{[]string{"build", "-t", "tar://o.tar", "--name", "x", "-c", "p", "extra"}, "Unmatched argument at index 7: 'extra'"},
		{[]string{"build", "-t", "tar://o.tar", "-c", "p", "a", "b"}, "Unmatched arguments from index 5: 'a', 'b'"},
		{[]string{"build", "-t", "x", "-t", "y"}, "option '--target' (<target-image>) should be specified only once"},
		{[]string{"build", "-t", "x", "--verbosity", "loud"}, "Invalid value for option '--verbosity': expected one of [quiet, error, warn, lifecycle, info, debug] (case-sensitive) but was 'loud'"},
		{[]string{"build", "-t", "x", "--console", "fancy"}, "Invalid value for option '--console': expected one of [auto, rich, plain] (case-sensitive) but was 'fancy'"},
		{[]string{"build", "-t", "x", "--http-trace=most"}, "Invalid value for option '--http-trace': expected one of [off, config, all] (case-sensitive) but was 'most'"},
		{[]string{"build", "-t", "x", "-p", "a"}, "Value for option option '--parameter' (<name>=<value>) should be in KEY=VALUE format but was a"},
		{[]string{"build", "-t", "x", "--from", "alpine"}, "Unknown option: '--from'"},
		{[]string{"build", "-t", "x", "--entrypoint", "/app"}, "Unknown option: '--entrypoint'"},
		{[]string{"build", "-t", "x", "-x"}, "Unknown option: '-x'"},
		{[]string{"build", "-t", "x", "--username", "u"}, "Error: Missing required argument(s): --password"},
		{[]string{"build", "-t", "x", "--password", "p"}, "Error: Missing required argument(s): --username=<username>"},
		{[]string{"build", "-t", "x", "--from-username", "u"}, "Error: Missing required argument(s): --from-password"},
		{[]string{"build", "-t", "x", "--credential-helper", "a", "--to-credential-helper", "b"}, "Error: " + credentialGroups + " are mutually exclusive (specify only one)"},
		{[]string{"build", "-t", "x", "--to-credential-helper", "a", "--to-username", "u", "--to-password", "p"},
			"Error: --to-credential-helper=<credential-helper> and [--to-username=<username> --to-password[=<password>]] are mutually exclusive (specify only one)"},
		{[]string{"bogus"}, "Unmatched argument at index 0: 'bogus'"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			code, out, errOut := runGib(t, dir, "", tc.args...)
			assert.Equal(t, 2, code)
			assert.Empty(t, out)
			assert.True(t, strings.HasPrefix(errOut, tc.want+"\n"), errOut)
			assert.Contains(t, errOut, "for more information on usage.")
		})
	}
}

func TestBuild_WritesATarballAndItsMetadataAsJibDoes(t *testing.T) {
	dir := project(t, appBuildFile)

	code, out, errOut := runGib(t, dir, "", "build", "-t", "tar://o.tar", "--name", "test/x", "-c", "p",
		"--additional-tags", "b", "--additional-tags", "a,b", "--image-metadata-out", "m.json")

	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "Getting scratch base image...\nBuilding app layer...\nBuilding image to tar file...\n", out)
	assert.Empty(t, errOut)
	var m []struct{ RepoTags []string }
	require.NoError(t, json.Unmarshal(tarFile(t, filepath.Join(dir, "o.tar"), "manifest.json"), &m))
	assert.Equal(t, []string{"test/x:latest", "test/x:b", "test/x:a"}, m[0].RepoTags)
	meta, err := os.ReadFile(filepath.Join(dir, "m.json"))
	require.NoError(t, err)
	assert.Regexp(t, `^\{"image":"test/x","imageId":"sha256:[0-9a-f]{64}","imageDigest":"sha256:[0-9a-f]{64}","tags":\["a","b","latest"\],"imagePushed":false\}$`, string(meta))
}

func TestBuild_ReadsTheBuildFileInTheContext(t *testing.T) {
	dir := project(t, appBuildFile)

	code, _, errOut := runGib(t, dir, "", "build", "-t", "tar://o.tar", "--name", "x", "-c", "p")
	assert.Equal(t, 0, code, errOut)

	code, _, errOut = runGib(t, dir, "", "build", "-t", "tar://o.tar", "--name", "x", "-c", "nope")
	assert.Equal(t, 1, code)
	assert.Equal(t, "[ERROR] The Build File YAML either does not exist or cannot be opened for reading: nope/jib.yaml\n", errOut)
}

func TestBuild_FailsAsJibFailsBeforeBuilding(t *testing.T) {
	dir := project(t, appBuildFile)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--name", "x", "-b", "p"}, "Build File YAML path is a not a file: p"},
		{[]string{"--name", "x", "-b", "p/jib.yaml", "-c", "nope"}, "contextRoot must be a directory, but nope is not."},
		{[]string{"-c", "p", "--name", "Bad Name"}, "Invalid image reference: Bad Name"},
		{[]string{"--name", "x", "-c", "p", "--additional-tags", "bad tag"}, "invalid tag 'bad tag'"},
	} {
		args := append([]string{"build", "-t", "tar://o.tar"}, tc.args...)
		code, out, errOut := runGib(t, dir, "", args...)
		assert.Equal(t, 1, code, tc.want)
		assert.Empty(t, out)
		assert.Equal(t, "[ERROR] "+tc.want+"\n", errOut)
	}
}

func TestBuild_AMissingCredentialHelperPathFailsARegistryBuild(t *testing.T) {
	dir := project(t, appBuildFile)

	code, _, errOut := runGib(t, dir, "", "build", "-t", "localhost:5000/x", "-c", "p", "--credential-helper", "./nope/helper")

	assert.Equal(t, 1, code)
	assert.Equal(t, "[ERROR] Specified credential helper was not found: ./nope/helper\n", errOut)
}

func TestBuild_QuietIsQuiet(t *testing.T) {
	dir := project(t, appBuildFile)

	code, out, errOut := runGib(t, dir, "", "build", "-t", "tar://o.tar", "--name", "x", "-c", "p", "--verbosity", "quiet")
	assert.Equal(t, 0, code)
	assert.Empty(t, out)
	assert.Empty(t, errOut)

	code, out, errOut = runGib(t, dir, "", "build", "-t", "tar://o.tar", "--name", "x", "-c", "nope", "--verbosity", "quiet")
	assert.Equal(t, 1, code)
	assert.Empty(t, out)
	assert.Empty(t, errOut)
}

func TestBuild_KeepsAParametersCommas(t *testing.T) {
	dir := project(t, appBuildFile+"labels:\n  list: ${list}\n")

	code, _, errOut := runGib(t, dir, "", "build", "-t", "tar://o.tar", "--name", "x", "-c", "p", "-p", "list=a,b")

	require.Equal(t, 0, code, errOut)
	var cfg struct {
		Config struct{ Labels map[string]string }
	}
	require.NoError(t, json.Unmarshal(tarFile(t, filepath.Join(dir, "o.tar"), "config.json"), &cfg))
	assert.Equal(t, "a,b", cfg.Config.Labels["list"])
}

func TestBuild_ExpandsArgumentFiles(t *testing.T) {
	dir := project(t, appBuildFile)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "args"), []byte("-t tar://o.tar\n# a comment\n--name \"test/x\" -c p\n"), 0o644))

	code, _, errOut := runGib(t, dir, "", "build", "@args")

	require.Equal(t, 0, code, errOut)
	assert.FileExists(t, filepath.Join(dir, "o.tar"))
}

func TestBuild_PromptsForAPasswordGivenNone(t *testing.T) {
	dir := project(t, appBuildFile)
	reg := registry.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "u" || p != "secret" {
			w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		reg.ServeHTTP(w, r)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	code, out, errOut := runGib(t, dir, "secret\n", "build", "-t", host+"/acme/app", "-c", "p",
		"--allow-insecure-registries", "--send-credentials-over-http", "--username", "u", "--password")

	require.Equal(t, 0, code, errOut)
	assert.True(t, strings.HasPrefix(out, "Enter value for --password (password for communicating with both target and base image registries): "), out)
	assert.Contains(t, out, "Using credentials from --username/--password for "+host+"/acme/app\n")
}

func TestBuild_LoadsADockerTargetWithDockerLoad(t *testing.T) {
	dir := project(t, appBuildFile)
	bin := t.TempDir()
	loaded := filepath.Join(bin, "loaded.tar")
	script := "#!/bin/sh\ncase \"$1\" in\ninfo) echo '{\"OSType\":\"linux\",\"Architecture\":\"x86_64\"}' ;;\nload) cat > " + loaded + " ;;\nesac\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	code, out, errOut := runGib(t, dir, "", "build", "-t", "docker://acme/app:1", "-c", "p", "--image-metadata-out", "m.json")

	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "Getting scratch base image...\nBuilding app layer...\nLoading to Docker daemon...\n", out)
	var m []struct{ RepoTags []string }
	require.NoError(t, json.Unmarshal(tarFile(t, loaded, "manifest.json"), &m))
	assert.Equal(t, []string{"acme/app:1"}, m[0].RepoTags)
	meta, err := os.ReadFile(filepath.Join(dir, "m.json"))
	require.NoError(t, err)
	assert.Contains(t, string(meta), `"image":"acme/app:1"`)
}

func TestVersion_IsPrintedByTheRootAlone(t *testing.T) {
	dir := t.TempDir()

	code, out, _ := runGib(t, dir, "", "-V")
	assert.Equal(t, 0, code)
	assert.NotEmpty(t, out)

	code, out, _ = runGib(t, dir, "", "build", "-V")
	assert.Equal(t, 0, code)
	assert.Empty(t, out, "Jib's build has no version of its own")
}
