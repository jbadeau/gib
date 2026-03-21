package gib

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileEntriesLayerBuilder_AddEntry(t *testing.T) {
	dir := testdataLayerDir(t)

	layer, err := NewFileEntriesLayerBuilder().
		SetName("test").
		AddEntry(filepath.Join(dir, "foo"), "/app/foo").
		Build()

	require.NoError(t, err)
	assert.Equal(t, "test", layer.Name)
	assert.Len(t, layer.Entries, 1)
	assert.Equal(t, "/app/foo", layer.Entries[0].DestinationPath)
	assert.Equal(t, os.FileMode(0644), layer.Entries[0].Permissions)
}

func TestFileEntriesLayerBuilder_AddEntryWithPermissions(t *testing.T) {
	dir := testdataLayerDir(t)

	layer, err := NewFileEntriesLayerBuilder().
		AddEntryWithPermissions(filepath.Join(dir, "foo"), "/app/foo", 0755).
		Build()

	require.NoError(t, err)
	assert.Len(t, layer.Entries, 1)
	assert.Equal(t, os.FileMode(0755), layer.Entries[0].Permissions)
}

func TestFileEntriesLayerBuilder_AddEntryRecursive(t *testing.T) {
	dir := testdataLayerDir(t)

	layer, err := NewFileEntriesLayerBuilder().
		SetName("recursive").
		AddEntryRecursive(dir, "/app").
		Build()

	require.NoError(t, err)
	assert.Greater(t, len(layer.Entries), 1)

	// Entries should be sorted
	for i := 1; i < len(layer.Entries); i++ {
		assert.LessOrEqual(t, layer.Entries[i-1].DestinationPath, layer.Entries[i].DestinationPath)
	}
}

func TestFileEntriesLayerBuilder_SortedOutput(t *testing.T) {
	dir := testdataLayerDir(t)

	layer, err := NewFileEntriesLayerBuilder().
		AddEntry(filepath.Join(dir, "foo"), "/z/foo").
		AddEntry(filepath.Join(dir, "a/b/bar"), "/a/bar").
		Build()

	require.NoError(t, err)
	assert.Equal(t, "/a/bar", layer.Entries[0].DestinationPath)
	assert.Equal(t, "/z/foo", layer.Entries[1].DestinationPath)
}

func TestFileEntriesLayerBuilder_InvalidDestination(t *testing.T) {
	dir := testdataLayerDir(t)

	_, err := NewFileEntriesLayerBuilder().
		AddEntry(filepath.Join(dir, "foo"), "relative/path").
		Build()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "absolute")
}

func TestFileEntriesLayerBuilder_MissingSource(t *testing.T) {
	_, err := NewFileEntriesLayerBuilder().
		AddEntry("/nonexistent/file", "/app/file").
		Build()

	assert.Error(t, err)
}

func TestFileEntriesLayerBuilder_EmptyDestination(t *testing.T) {
	dir := testdataLayerDir(t)

	_, err := NewFileEntriesLayerBuilder().
		AddEntry(filepath.Join(dir, "foo"), "").
		Build()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func testdataLayerDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("testdata/layers/simple")
	require.NoError(t, err)
	return dir
}
