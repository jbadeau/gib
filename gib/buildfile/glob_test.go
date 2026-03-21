package buildfile

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchFiles_NoPatterns(t *testing.T) {
	dir := excludesDir(t)

	files, err := matchFiles(dir, nil, nil)
	require.NoError(t, err)
	assert.Len(t, files, 4) // include.txt, exclude.me, sub/nested.txt, sub/nested.me
}

func TestMatchFiles_ExcludePattern(t *testing.T) {
	dir := excludesDir(t)

	files, err := matchFiles(dir, nil, []string{"**/*.me"})
	require.NoError(t, err)
	assert.Len(t, files, 2)

	for _, f := range files {
		assert.NotContains(t, f, ".me")
	}
}

func TestMatchFiles_IncludePattern(t *testing.T) {
	dir := excludesDir(t)

	files, err := matchFiles(dir, []string{"**/*.txt"}, nil)
	require.NoError(t, err)
	assert.Len(t, files, 2) // include.txt and sub/nested.txt
}

func TestMatchFiles_IncludeAndExclude(t *testing.T) {
	dir := excludesDir(t)

	// Include all txt, but exclude nested ones
	files, err := matchFiles(dir, []string{"**/*.txt"}, []string{"sub/**"})
	require.NoError(t, err)
	assert.Len(t, files, 1) // only include.txt
}

func TestMatchFiles_SpecificFile(t *testing.T) {
	dir := excludesDir(t)

	files, err := matchFiles(dir, []string{"include.txt"}, nil)
	require.NoError(t, err)
	assert.Len(t, files, 1)
}

func TestIsSingleFile(t *testing.T) {
	assert.True(t, isSingleFile("file.txt"))
	assert.True(t, isSingleFile("path/to/file"))
	assert.False(t, isSingleFile("dir/"))
}

func excludesDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "testdata", "layers", "with_excludes"))
	require.NoError(t, err)
	return dir
}
