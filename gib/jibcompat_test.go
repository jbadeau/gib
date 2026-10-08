package gib_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/jbadeau/gib"
	"github.com/jbadeau/gib/buildfile"
	"github.com/stretchr/testify/require"
)

// The Jib compatibility cases: each build file is built by gib and
// compared with what the Jib CLI built from it, recorded under
// testdata/jibcompat. `go test -run TestJibCompat -update-jib` records
// them again with the jib on PATH (or $JIB), which needs Java.
var updateJib = flag.Bool("update-jib", false, "record testdata/jibcompat with the Jib CLI")

// jibCases are build files over a base image that sets every field Jib
// carries over, and some it does not. ${base} is that image's tarball.
var jibCases = map[string]string{
	"inherits-everything": `
from: {image: "tar://${base}"}
`,
	"entrypoint-clears-cmd": `
from: {image: "tar://${base}"}
entrypoint: ["/app"]
`,
	"cmd-keeps-entrypoint": `
from: {image: "tar://${base}"}
cmd: ["--flag"]
`,
	"overrides": `
from: {image: "tar://${base}"}
creationTime: "1600000000000"
environment: {A: "2", NEW_B: "3", NEW_A: "4"}
labels: {base: "over", added: "x"}
exposedPorts: ["9090", "53/udp"]
volumes: ["/extra"]
user: "2000"
workingDirectory: /work
entrypoint: ["/app", "run"]
cmd: ["--x"]
`,
	"oci": `
from: {image: "tar://${base}"}
format: OCI
`,
	"layers": `
from: {image: "tar://${base}"}
layers:
  entries:
    - name: app
      files:
        - {src: hello.txt, dest: /app/hello.txt}
    - name: nothing
      files:
        - {src: dir, dest: /none, includes: ["**/*.nomatch"]}
`,
	"scratch": `
layers:
  entries:
    - name: app
      files:
        - {src: hello.txt, dest: /app/hello.txt}
`,
}

// jibBase writes the base image every case builds on: a docker-save
// tarball both Jib and gib read.
func jibBase(t *testing.T, dir string) string {
	t.Helper()
	content := tarOf1(t, "base.txt", "base")
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(content)), nil
	})
	require.NoError(t, err)
	img, err := mutate.AppendLayers(empty.Image, layer)
	require.NoError(t, err)
	cfg, err := img.ConfigFile()
	require.NoError(t, err)
	cfg = cfg.DeepCopy()
	cfg.Architecture, cfg.OS = "amd64", "linux"
	cfg.Author = "someone"
	cfg.Created = v1.Time{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	cfg.History = nil
	cfg.Config = v1.Config{
		Entrypoint:   []string{"/base-ep"},
		Cmd:          []string{"base-cmd"},
		Env:          []string{"PATH=/usr/bin", "A=1"},
		Labels:       map[string]string{"base": "b"},
		ExposedPorts: map[string]struct{}{"8080/tcp": {}},
		Volumes:      map[string]struct{}{"/data": {}},
		User:         "1000",
		WorkingDir:   "/base",
		Healthcheck:  &v1.HealthConfig{Test: []string{"CMD", "true"}},
		StopSignal:   "SIGTERM",
		OnBuild:      []string{"RUN x"},
	}
	img, err = mutate.ConfigFile(img, cfg)
	require.NoError(t, err)
	file := filepath.Join(dir, "base.tar")
	tag, err := name.NewTag("base/image:1")
	require.NoError(t, err)
	require.NoError(t, tarball.WriteToFile(file, tag, img))
	return file
}

// jibContext lays out what the cases' layers copy.
func jibContext(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "dir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dir", "a.txt"), []byte("a"), 0o644))
	return dir
}

func TestJibCompat(t *testing.T) {
	names := make([]string, 0, len(jibCases))
	for n := range jibCases {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			ctx := jibContext(t)
			base := jibBase(t, t.TempDir())
			file := filepath.Join(ctx, "jib.yaml")
			require.NoError(t, os.WriteFile(file, []byte("apiVersion: jib/v1alpha1\nkind: BuildFile\n"+jibCases[name]), 0o644))
			golden := filepath.Join("testdata", "jibcompat", name+".json")

			if *updateJib {
				out := filepath.Join(t.TempDir(), "jib.tar")
				jib := os.Getenv("JIB")
				if jib == "" {
					jib = "jib"
				}
				cmd := exec.Command(jib, "build", "-b", file, "-c", ctx, "-t", "tar://"+out, "--name", "test/"+name, "-p", "base="+base)
				msg, err := cmd.CombinedOutput()
				require.NoError(t, err, string(msg))
				require.NoError(t, os.MkdirAll(filepath.Dir(golden), 0o755))
				require.NoError(t, os.WriteFile(golden, summarize(t, out), 0o644))
				return
			}

			spec, err := buildfile.Parse(file, map[string]string{"base": base})
			require.NoError(t, err)
			b, err := buildfile.Convert(spec, ctx, nil)
			require.NoError(t, err)
			out := filepath.Join(t.TempDir(), "gib.tar")
			_, err = b.Containerize(context.Background(), gib.ToTar(out, gib.WithTarImageName("test/"+name)))
			require.NoError(t, err)

			want, err := os.ReadFile(golden)
			require.NoError(t, err, "record it with -update-jib")
			require.JSONEq(t, string(want), string(summarize(t, out)))
		})
	}
}

// summary is what Jib and gib must agree on for an image: everything in
// its manifest and config but the bytes no two tools write alike, the
// compressed layers, and the tool's own name in the history.
type summary struct {
	ManifestMediaType types.MediaType   `json:"manifestMediaType"`
	ConfigMediaType   types.MediaType   `json:"configMediaType"`
	LayerMediaTypes   []types.MediaType `json:"layerMediaTypes"`
	Architecture      string            `json:"architecture"`
	OS                string            `json:"os"`
	Created           string            `json:"created"`
	Config            summaryConfig     `json:"config"`
	History           []v1.History      `json:"history"`
	DiffIDs           int               `json:"diffIDs"`
}

type summaryConfig struct {
	Env          []string          `json:"env"`
	Entrypoint   []string          `json:"entrypoint"`
	Cmd          []string          `json:"cmd"`
	Labels       map[string]string `json:"labels"`
	ExposedPorts []string          `json:"exposedPorts"`
	Volumes      []string          `json:"volumes"`
	User         string            `json:"user"`
	WorkingDir   string            `json:"workingDir"`
	Healthcheck  *v1.HealthConfig  `json:"healthcheck"`
	Other        []string          `json:"other"`
}

var tool = regexp.MustCompile(`^(jib-cli|gib):.*$`)

func summarize(t *testing.T, file string) []byte {
	t.Helper()
	manifest, cfgRaw := imageIn(t, file)
	var cfg v1.ConfigFile
	require.NoError(t, json.Unmarshal(cfgRaw, &cfg))
	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(cfgRaw, &top))
	var rawConfig map[string]json.RawMessage
	if c, ok := top["config"]; ok {
		require.NoError(t, json.Unmarshal(c, &rawConfig))
	}
	s := summary{
		ManifestMediaType: manifest.MediaType,
		ConfigMediaType:   manifest.Config.MediaType,
		Architecture:      cfg.Architecture,
		OS:                cfg.OS,
		Created:           cfg.Created.UTC().Format(time.RFC3339Nano),
		DiffIDs:           len(cfg.RootFS.DiffIDs),
	}
	for _, l := range manifest.Layers {
		s.LayerMediaTypes = append(s.LayerMediaTypes, l.MediaType)
	}
	for _, h := range cfg.History {
		h.CreatedBy = tool.ReplaceAllString(h.CreatedBy, "<tool>")
		h.Created = v1.Time{Time: h.Created.UTC()}
		s.History = append(s.History, h)
	}
	c := cfg.Config
	// Jib writes empty maps and lists where gib, through
	// go-containerregistry, omits them; both mean none.
	if len(c.Labels) == 0 {
		c.Labels = nil
	}
	env := append([]string(nil), c.Env...)
	sort.Strings(env)
	s.Config = summaryConfig{Env: env, Entrypoint: c.Entrypoint, Cmd: c.Cmd, Labels: c.Labels,
		ExposedPorts: keys(c.ExposedPorts), Volumes: keys(c.Volumes), User: c.User, WorkingDir: c.WorkingDir, Healthcheck: c.Healthcheck}
	known := map[string]bool{"Env": true, "Entrypoint": true, "Cmd": true, "Labels": true, "ExposedPorts": true,
		"Volumes": true, "User": true, "WorkingDir": true, "Healthcheck": true}
	for k, v := range rawConfig {
		if !known[k] && string(v) != "null" && string(v) != `""` && string(v) != "false" {
			s.Config.Other = append(s.Config.Other, k)
		}
	}
	sort.Strings(s.Config.Other)
	out, err := json.MarshalIndent(s, "", "  ")
	require.NoError(t, err)
	return append(out, '\n')
}

// imageIn reads the one image of an image tarball, in either of Jib's
// layouts: its manifest and its config's bytes.
func imageIn(t *testing.T, file string) (*v1.Manifest, []byte) {
	t.Helper()
	files := map[string][]byte{}
	f, err := os.Open(file)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	r := tar.NewReader(f)
	for {
		h, err := r.Next()
		if err != nil {
			break
		}
		var buf bytes.Buffer
		_, err = buf.ReadFrom(r)
		require.NoError(t, err)
		files[h.Name] = buf.Bytes()
	}
	if idx, ok := files["index.json"]; ok {
		var index v1.IndexManifest
		require.NoError(t, json.Unmarshal(idx, &index))
		raw := files["blobs/sha256/"+index.Manifests[0].Digest.Hex]
		var m v1.Manifest
		require.NoError(t, json.Unmarshal(raw, &m))
		return &m, files["blobs/sha256/"+m.Config.Digest.Hex]
	}
	img, err := tarball.ImageFromPath(file, nil)
	require.NoError(t, err)
	m, err := img.Manifest()
	require.NoError(t, err)
	cfg, err := img.RawConfigFile()
	require.NoError(t, err)
	return m, cfg
}

func keys(m map[string]struct{}) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func tarOf1(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}))
	_, err := tw.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	return buf.Bytes()
}
