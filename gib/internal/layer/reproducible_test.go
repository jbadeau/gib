package layer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildReproducibleLayer_SortedEntries(t *testing.T) {
	dir := testdataDir(t, "simple")

	entries := []Entry{
		{SourcePath: filepath.Join(dir, "foo"), DestinationPath: "/app/foo", Permissions: 0644, ModificationTime: 1000, Ownership: "0:0"},
		{SourcePath: filepath.Join(dir, "a/b/bar"), DestinationPath: "/app/a/b/bar", Permissions: 0644, ModificationTime: 1000, Ownership: "0:0"},
		{SourcePath: filepath.Join(dir, "c/cat"), DestinationPath: "/app/c/cat", Permissions: 0644, ModificationTime: 1000, Ownership: "0:0"},
	}

	layer, err := BuildReproducibleLayer(entries)
	require.NoError(t, err)

	headers := readTarHeaders(t, layer)
	names := make([]string, len(headers))
	for i, h := range headers {
		names[i] = h.Name
	}

	// Should be sorted: parent dirs first (created automatically), then files in order
	assert.Contains(t, names, "/app/a/b/bar")
	assert.Contains(t, names, "/app/c/cat")
	assert.Contains(t, names, "/app/foo")

	// Verify files are in sorted order (by destination path)
	fileNames := filterFiles(headers)
	for i := 1; i < len(fileNames); i++ {
		assert.Less(t, fileNames[i-1], fileNames[i], "files should be sorted by destination path")
	}
}

func TestBuildReproducibleLayer_NormalizedTimestamps(t *testing.T) {
	dir := testdataDir(t, "simple")

	entries := []Entry{
		{SourcePath: filepath.Join(dir, "foo"), DestinationPath: "/app/foo", Permissions: 0644, ModificationTime: 1000, Ownership: "0:0"},
	}

	layer, err := BuildReproducibleLayer(entries)
	require.NoError(t, err)

	headers := readTarHeaders(t, layer)
	for _, h := range headers {
		assert.Equal(t, int64(1), h.ModTime.Unix(), "timestamp should be epoch+1s for entry %s", h.Name)
	}
}

func TestBuildReproducibleLayer_Permissions(t *testing.T) {
	dir := testdataDir(t, "simple")

	entries := []Entry{
		{SourcePath: filepath.Join(dir, "foo"), DestinationPath: "/app/foo", Permissions: 0755, ModificationTime: 1000, Ownership: "0:0"},
	}

	layer, err := BuildReproducibleLayer(entries)
	require.NoError(t, err)

	headers := readTarHeaders(t, layer)
	for _, h := range headers {
		if h.Name == "/app/foo" {
			assert.Equal(t, int64(0755), h.Mode)
		}
	}
}

func TestBuildReproducibleLayer_Ownership(t *testing.T) {
	dir := testdataDir(t, "simple")

	entries := []Entry{
		{SourcePath: filepath.Join(dir, "foo"), DestinationPath: "/app/foo", Permissions: 0644, ModificationTime: 1000, Ownership: "1000:2000"},
	}

	layer, err := BuildReproducibleLayer(entries)
	require.NoError(t, err)

	headers := readTarHeaders(t, layer)
	for _, h := range headers {
		if h.Name == "/app/foo" {
			assert.Equal(t, 1000, h.Uid)
			assert.Equal(t, 2000, h.Gid)
		}
	}
}

func TestBuildReproducibleLayer_ParentDirCreation(t *testing.T) {
	dir := testdataDir(t, "simple")

	entries := []Entry{
		{SourcePath: filepath.Join(dir, "a/b/bar"), DestinationPath: "/deep/nested/path/bar", Permissions: 0644, ModificationTime: 1000, Ownership: "0:0"},
	}

	layer, err := BuildReproducibleLayer(entries)
	require.NoError(t, err)

	headers := readTarHeaders(t, layer)
	names := make([]string, len(headers))
	for i, h := range headers {
		names[i] = h.Name
	}

	assert.Contains(t, names, "/deep/")
	assert.Contains(t, names, "/deep/nested/")
	assert.Contains(t, names, "/deep/nested/path/")
	assert.Contains(t, names, "/deep/nested/path/bar")
}

func TestBuildReproducibleLayer_Deterministic(t *testing.T) {
	dir := testdataDir(t, "simple")

	entries := []Entry{
		{SourcePath: filepath.Join(dir, "foo"), DestinationPath: "/app/foo", Permissions: 0644, ModificationTime: 1000, Ownership: "0:0"},
		{SourcePath: filepath.Join(dir, "a/b/bar"), DestinationPath: "/app/a/b/bar", Permissions: 0644, ModificationTime: 1000, Ownership: "0:0"},
	}

	layer1, err := BuildReproducibleLayer(entries)
	require.NoError(t, err)

	layer2, err := BuildReproducibleLayer(entries)
	require.NoError(t, err)

	digest1, err := layer1.Digest()
	require.NoError(t, err)
	digest2, err := layer2.Digest()
	require.NoError(t, err)

	assert.Equal(t, digest1, digest2, "layers should produce identical digests")
}

func TestBuildReproducibleLayer_DirectoryEntry(t *testing.T) {
	dir := testdataDir(t, "simple")
	aDir := filepath.Join(dir, "a")

	entries := []Entry{
		{SourcePath: aDir, DestinationPath: "/app/a/", Permissions: 0755, ModificationTime: 1000, Ownership: "0:0"},
	}

	layer, err := BuildReproducibleLayer(entries)
	require.NoError(t, err)

	headers := readTarHeaders(t, layer)
	found := false
	for _, h := range headers {
		if h.Name == "/app/a/" {
			found = true
			assert.Equal(t, byte(tar.TypeDir), h.Typeflag)
			assert.Equal(t, int64(0755), h.Mode)
		}
	}
	assert.True(t, found, "should contain directory entry /app/a/")
}

// Helpers

func testdataDir(t *testing.T, name string) string {
	t.Helper()
	// Navigate from internal/layer/ to project root
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "layers", name))
	require.NoError(t, err)
	_, err = os.Stat(dir)
	require.NoError(t, err, "testdata directory %s must exist", dir)
	return dir
}

func readTarHeaders(t *testing.T, l interface{ Compressed() (io.ReadCloser, error) }) []*tar.Header {
	t.Helper()
	rc, err := l.Compressed()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()

	var buf bytes.Buffer
	_, err = io.Copy(&buf, rc)
	require.NoError(t, err)

	gr, err := gzip.NewReader(&buf)
	require.NoError(t, err)
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	var headers []*tar.Header
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		headers = append(headers, hdr)
	}
	return headers
}

func filterFiles(headers []*tar.Header) []string {
	var names []string
	for _, h := range headers {
		if h.Typeflag == tar.TypeReg {
			names = append(names, h.Name)
		}
	}
	return names
}
