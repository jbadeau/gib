package layer

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These hold the layer's rules one at a time; what layers hold, against
// layers Jib built, is TestJibCompat's.

func TestTimesAreKeptToTheMillisecond(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))

	raw, err := Tar([]Entry{{SourcePath: f, DestinationPath: "/f", ModificationTime: 1050}})
	require.NoError(t, err)

	h, err := tar.NewReader(bytes.NewReader(raw)).Next()
	require.NoError(t, err)
	assert.Equal(t, int64(1_050_000_000), h.ModTime.UnixNano())
}

func TestLongNamesAreKept(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))
	long := "/" + strings.Repeat("d", 120) + "/f"

	raw, err := Tar([]Entry{{SourcePath: f, DestinationPath: long}})
	require.NoError(t, err)

	r := tar.NewReader(bytes.NewReader(raw))
	var names []string
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, h.Name)
	}
	assert.Contains(t, names, long[1:])
}

func TestTheFirstEntryOfANameWins(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	require.NoError(t, os.WriteFile(a, []byte("first"), 0o644))
	require.NoError(t, os.WriteFile(b, []byte("second"), 0o644))

	raw, err := Tar([]Entry{{SourcePath: a, DestinationPath: "/x"}, {SourcePath: b, DestinationPath: "/x"}})
	require.NoError(t, err)

	r := tar.NewReader(bytes.NewReader(raw))
	for {
		h, err := r.Next()
		require.NoError(t, err)
		if h.Name == "x" {
			content, err := io.ReadAll(r)
			require.NoError(t, err)
			assert.Equal(t, "first", string(content))
			return
		}
	}
}

func TestEntriesAreRelativeAndTyped(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o600))

	raw, err := Tar([]Entry{{SourcePath: f, DestinationPath: "/app/bin/f", Permissions: 0o750, ModificationTime: 1000}})
	require.NoError(t, err)

	r := tar.NewReader(bytes.NewReader(raw))
	got := map[string]int64{}
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		got[h.Name] = h.Mode
	}
	assert.Equal(t, map[string]int64{"app/": 0o40755, "app/bin/": 0o40755, "app/bin/f": 0o100750}, got)
}

func TestAFIFOIsRefused(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip(err)
	}
	_, err := Tar([]Entry{{SourcePath: fifo, DestinationPath: "/fifo"}})
	assert.ErrorContains(t, err, "neither a file nor a directory")
}

func TestTheSameEntriesAreTheSameBytes(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"c", "a", "b"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644))
	}
	entries := []Entry{
		{SourcePath: filepath.Join(dir, "c"), DestinationPath: "/z/c"},
		{SourcePath: filepath.Join(dir, "a"), DestinationPath: "/a"},
		{SourcePath: filepath.Join(dir, "b"), DestinationPath: "/m/b"},
	}
	first, err := Tar(entries)
	require.NoError(t, err)
	for range 10 {
		again, err := Tar(entries)
		require.NoError(t, err)
		require.Equal(t, first, again)
	}
}
