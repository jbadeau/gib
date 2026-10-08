package gib_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/jbadeau/gib"
	"github.com/jbadeau/gib/buildfile"
	"github.com/stretchr/testify/require"
)

// The build file cases: whole build files, each run in the directory
// that holds the base image's tarball, base.tar, which ${base} names.

// jibBuildFiles are build files Jib builds; gib must build what Jib
// built, recorded in testdata/jibcompat/<case>.json.
var jibBuildFiles = map[string]string{
	"any-api-version": `apiVersion: anything
kind: BuildFile
user: "1"
`,
	"registry-scratch": head + `from: {image: "registry://scratch"}
layers: {entries: [{name: app, files: [{src: hello.txt, dest: /hello.txt}]}]}
`,
	"tar-relative-to-working-directory": head + `from: {image: "tar://base.tar"}
`,
	"port-ranges": head + `from: {image: "tar://${base}"}
exposedPorts: ["8000-8002/udp", "9000", "8001/udp"]
`,
	"absolute-unix-paths": head + `from: {image: "tar://${base}"}
volumes: ["/a//b/", "/a/b", "/c/"]
workingDirectory: "/w//x/"
layers:
  entries:
    - name: app
      files:
        - {src: hello.txt, dest: "/app//x/hello.txt"}
        - {src: hello.txt, dest: "/opt//"}
`,
	"parameters": head + `user: "${u:-nobody}"
cmd: ["${missing:-}x", "a$${b}c", "$${x}"]
entrypoint: ["${y"]
`,
	"times": head + `creationTime: "2020-01-01T01:00:00.123456789+01:00"
layers:
  properties: {timestamp: "2020-06-01t00:00:00.5z"}
  entries:
    - name: app
      properties: {timestamp: "2021-03-04T05:06+0200"}
      files:
        - {src: hello.txt, dest: /a.txt}
        - {src: hello.txt, dest: /b.txt, properties: {timestamp: "2022-01-01T00:00:00Z[Europe/Paris]"}}
`,
	"creation-time-zoned": head + `creationTime: "2020-10-25T02:30:00+01:00[Europe/Paris]"
`,
	"scalars-as-written": head + `user: 0644
entrypoint: [0x1F, yes, 1_000, ~x, 1.50]
`,
	"docker-base": head + `from: {image: "docker://gib-jibcompat/base:1"}
`,
}

// jibRefusals are build files Jib refuses; gib must refuse them too.
// testdata/jibcompat/refused/<case>.json records Jib's error.
var jibRefusals = map[string]string{
	"unknown-property":          head + "entryPoint: [a]\n",
	"unknown-copy-property":     head + "layers: {entries: [{name: a, files: [{src: hello.txt, dest: /h, mode: x}]}]}\n",
	"format-not-an-enum":        head + "format: docker\n",
	"empty-user":                head + "user: ''\n",
	"empty-environment-value":   head + "environment: {A: ' '}\n",
	"null-command-entry":        head + "cmd: [a, ~]\n",
	"base-of-no-image":          head + "from: {}\n",
	"base-of-empty-image":       head + "from: {image: ''}\n",
	"platform-without-os":       head + "from: {image: busybox, platforms: [{architecture: arm64}]}\n",
	"null-platform":             head + "from: {image: busybox, platforms: [~]}\n",
	"port-upper-case":           head + "exposedPorts: [80/TCP]\n",
	"port-signed":               head + "exposedPorts: ['+80']\n",
	"port-range-backwards":      head + "exposedPorts: [9-8]\n",
	"port-zero":                 head + "exposedPorts: ['0']\n",
	"relative-volume":           head + "volumes: [data]\n",
	"relative-working-dir":      head + "workingDirectory: app\n",
	"relative-dest":             head + "layers: {entries: [{name: a, files: [{src: hello.txt, dest: app}]}]}\n",
	"date-only-creation-time":   head + "creationTime: 2020-01-01\n",
	"zoneless-creation-time":    head + "creationTime: 2020-01-01T00:00:00\n",
	"bad-timestamp":             head + "layers: {properties: {timestamp: soon}, entries: [{name: a, files: [{src: hello.txt, dest: /h}]}]}\n",
	"bad-permissions":           head + "layers: {properties: {filePermissions: '888'}, entries: [{name: a, files: [{src: hello.txt, dest: /h}]}]}\n",
	"archive-layer":             head + "layers: {entries: [{name: a, archive: a.tar}]}\n",
	"empty-entries":             head + "layers: {entries: []}\n",
	"layer-without-name":        head + "layers: {entries: [{files: [{src: hello.txt, dest: /h}]}]}\n",
	"layer-without-files":       head + "layers: {entries: [{name: a}]}\n",
	"layer-of-no-files":         head + "layers: {entries: [{name: a, files: []}]}\n",
	"null-layer":                head + "layers: {entries: [~]}\n",
	"null-copy":                 head + "layers: {entries: [{name: a, files: [~]}]}\n",
	"empty-src":                 head + "layers: {entries: [{name: a, files: [{src: '', dest: /h}]}]}\n",
	"undefined-parameter":       head + "user: ${who}\n",
	"empty-api-version":         "apiVersion: ''\nkind: BuildFile\n",
	"wrong-kind":                "apiVersion: v\nkind: Build\n",
	"includes-on-a-single-file": head + "layers: {entries: [{name: a, files: [{src: hello.txt, dest: /h, includes: ['**']}]}]}\n",
}

// gibRefusals are build files Jib builds that gib refuses, where Jib
// strays from the standard: YAML forbids a key twice in a mapping, and
// ISO 8601 has no 30 February nor two offsets. A layer entry's
// properties are read as written, so an unquoted 0644 there is the
// four digits it is at the top level, not the octal 420 Jib's Jackson
// tree makes of it. testdata/jibcompat/deviations/<case>.json records
// what Jib built.
var gibRefusals = map[string]string{
	"key-given-twice":     head + "user: a\nuser: b\n",
	"thirtieth-february":  head + "creationTime: 2021-02-30T00:00:00Z\n",
	"two-offsets":         head + "creationTime: 2020-01-01T00:00:00Z+0000\n",
	"unquoted-octal-mode": head + "layers: {entries: [{name: a, properties: {filePermissions: 0644}, files: [{src: hello.txt, dest: /h}]}]}\n",
}

const head = "apiVersion: jib/v1alpha1\nkind: BuildFile\n"

func TestJibBuildFiles(t *testing.T) {
	for _, c := range sorted(jibBuildFiles) {
		t.Run(c, func(t *testing.T) {
			golden, err := filepath.Abs(filepath.Join("testdata", "jibcompat", c+".json"))
			require.NoError(t, err)
			r := runCase(t, c, jibBuildFiles[c])
			if *updateJib {
				require.NoError(t, r.jibErr, r.jibOut)
				require.NoError(t, os.WriteFile(golden, summarize(t, r.jibTar), 0o644))
				return
			}
			require.NoError(t, r.gibErr)
			want, err := os.ReadFile(golden)
			require.NoError(t, err, "record it with -update-jib")
			require.JSONEq(t, string(want), string(summarize(t, r.gibTar)))
		})
	}
}

func TestJibRefusals(t *testing.T) {
	for _, c := range sorted(jibRefusals) {
		t.Run(c, func(t *testing.T) {
			golden, err := filepath.Abs(filepath.Join("testdata", "jibcompat", "refused", c+".json"))
			require.NoError(t, err)
			r := runCase(t, c, jibRefusals[c])
			if *updateJib {
				require.Error(t, r.jibErr, "Jib built it")
				writeJSON(t, golden, map[string]string{"error": errorLine(r.jibOut)})
				return
			}
			_, err = os.Stat(golden)
			require.NoError(t, err, "record it with -update-jib")
			require.Error(t, r.gibErr, "gib built what Jib refuses")
		})
	}
}

func TestGibRefusals(t *testing.T) {
	for _, c := range sorted(gibRefusals) {
		t.Run(c, func(t *testing.T) {
			golden, err := filepath.Abs(filepath.Join("testdata", "jibcompat", "deviations", c+".json"))
			require.NoError(t, err)
			r := runCase(t, c, gibRefusals[c])
			if *updateJib {
				require.NoError(t, r.jibErr, r.jibOut)
				require.NoError(t, os.MkdirAll(filepath.Dir(golden), 0o755))
				require.NoError(t, os.WriteFile(golden, summarize(t, r.jibTar), 0o644))
				return
			}
			_, err = os.Stat(golden)
			require.NoError(t, err, "record it with -update-jib")
			require.Error(t, r.gibErr)
		})
	}
}

type result struct {
	jibTar, jibOut string
	jibErr         error
	gibTar         string
	gibErr         error
}

// runCase builds a build file with Jib, when recording, or else with
// gib, in the directory of the base image's tarball.
func runCase(t *testing.T, c, file string) result {
	ctx := jibContext(t)
	base := jibBase(t, t.TempDir())
	if strings.HasPrefix(c, "docker-") {
		loadIntoDocker(t, base)
	}
	wd := filepath.Dir(base)
	t.Chdir(wd)
	path := filepath.Join(ctx, "jib.yaml")
	require.NoError(t, os.WriteFile(path, []byte(file), 0o644))
	params := map[string]string{"base": base, "ctx": ctx}
	var r result
	if *updateJib {
		jib := os.Getenv("JIB")
		if jib == "" {
			jib = "jib"
		}
		r.jibTar = filepath.Join(t.TempDir(), "jib.tar")
		cmd := exec.Command(jib, "build", "-b", path, "-c", ctx, "-t", "tar://"+r.jibTar, "--name", "test/"+c,
			"-p", "base="+base, "-p", "ctx="+ctx)
		cmd.Dir = wd
		out, err := cmd.CombinedOutput()
		r.jibOut, r.jibErr = string(out), err
		return r
	}
	r.gibTar = filepath.Join(t.TempDir(), "gib.tar")
	r.gibErr = func() error {
		spec, err := buildfile.Parse(path, params)
		if err != nil {
			return err
		}
		b, err := buildfile.Convert(spec, ctx, nil)
		if err != nil {
			return err
		}
		_, err = b.Containerize(context.Background(), gib.ToTar(r.gibTar, gib.WithTarImageName("test/"+c)))
		return err
	}()
	return r
}

// loadIntoDocker puts the base image into the Docker daemon as
// gib-jibcompat/base:1, or skips the case when there is no daemon.
func loadIntoDocker(t *testing.T, base string) {
	t.Helper()
	img, err := tarball.ImageFromPath(base, nil)
	require.NoError(t, err)
	tag, err := name.NewTag("gib-jibcompat/base:1")
	require.NoError(t, err)
	if _, err := daemon.Write(tag, img); err != nil {
		t.Skipf("no Docker daemon: %v", err)
	}
}

func errorLine(out string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "[ERROR]") {
			return strings.TrimSpace(strings.TrimPrefix(l, "[ERROR]"))
		}
	}
	return strings.TrimSpace(out)
}

func writeJSON(t *testing.T, file string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	require.NoError(t, os.WriteFile(file, append(b, '\n'), 0o644))
}

func sorted(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
