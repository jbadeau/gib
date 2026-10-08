package layer

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These hold the parts of Jib's layer bytes one at a time; the whole of
// them, against layers Jib built, is TestJibCompat's.

func TestPAXRecordsComeInJavasHashMapOrder(t *testing.T) {
	m := &javaMap{}
	for _, k := range []string{"path", "mtime", "atime", "ctime", "LIBARCHIVE.creationtime"} {
		m.put(k, "1")
	}
	want := "10 path=1\n11 atime=1\n11 ctime=1\n11 mtime=1\n29 LIBARCHIVE.creationtime=1\n"
	assert.Equal(t, want, string(m.encode()))
}

func TestPAXRecordLengthsCountThemselves(t *testing.T) {
	m := &javaMap{}
	m.put("path", string(bytes.Repeat([]byte("a"), 95)))
	line := string(m.encode())
	assert.Equal(t, "105 path=", line[:9], "the length includes its own three digits")
	assert.Len(t, line, 105)
}

func TestJibsTimeIsItsUnpaddedNanoseconds(t *testing.T) {
	cases := []struct {
		millis      int64
		secs, nanos int64
		pax         string
	}{
		{1000, 1, 0, "1"},
		{1500, 1, 500_000_000, "1.5000000"},
		{1050, 1, 500_000_000, "1.5000000"}, // Jib writes "1.50000000"
		{1005, 1, 500_000_000, "1.5000000"},
		{1234, 1, 234_000_000, "1.2340000"},
	}
	for _, c := range cases {
		secs, nanos := jibTime(c.millis)
		assert.Equal(t, [2]int64{c.secs, c.nanos}, [2]int64{secs, nanos}, "%dms", c.millis)
		assert.Equal(t, c.pax, paxTime(secs, nanos), "%dms", c.millis)
	}
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
