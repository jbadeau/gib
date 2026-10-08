package gib_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jbadeau/gib"
	"github.com/jbadeau/gib/buildfile"
	"github.com/stretchr/testify/require"
)

// The same build file over the same files is the same image, however
// the files came to be on disk: a build cache keyed on its inputs can
// trust the image it recorded. The includes below are where a walk's
// order decides which directory entries carry the copy's permissions.
const reproducible = `apiVersion: jib/v1alpha1
kind: BuildFile
environment: {B: "2", A: "1", C: "3"}
layers:
  entries:
    - name: app
      properties: {directoryPermissions: "700"}
      files:
        - {src: src, dest: /app, includes: ["**/*.txt"]}
`

func TestTheSameInputsAreTheSameImage(t *testing.T) {
	files := []string{"src/a/x.txt", "src/a/b/y.txt", "src/a/b/c/z.txt", "src/m/n.txt", "src/a/skip.me"}
	var digests []string
	for _, order := range [][]string{files, reversed(files)} {
		ctx := t.TempDir()
		for _, f := range order {
			p := filepath.Join(ctx, f)
			require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
			require.NoError(t, os.WriteFile(p, []byte(f), 0o644))
		}
		file := filepath.Join(ctx, "jib.yaml")
		require.NoError(t, os.WriteFile(file, []byte(reproducible), 0o644))
		for range 3 {
			spec, err := buildfile.Parse(file, nil)
			require.NoError(t, err)
			b, err := buildfile.Convert(spec, ctx, nil)
			require.NoError(t, err)
			c, err := b.Containerize(context.Background(), gib.ToTar(filepath.Join(t.TempDir(), "image.tar"), gib.WithTarImageName("app")))
			require.NoError(t, err)
			digests = append(digests, c.Digest.String())
		}
	}
	for _, d := range digests[1:] {
		require.Equal(t, digests[0], d)
	}
}

func reversed(s []string) []string {
	out := slices.Clone(s)
	slices.Reverse(out)
	return out
}
