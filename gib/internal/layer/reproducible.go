package layer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// Entry represents a single file to add to a layer.
type Entry struct {
	SourcePath       string
	DestinationPath  string
	Permissions      fs.FileMode
	ModificationTime int64  // millis since epoch
	Ownership        string // "uid:gid"
}

// BuildReproducibleLayer creates a v1.Layer from file entries with
// deterministic tar output (sorted entries, normalized timestamps).
func BuildReproducibleLayer(entries []Entry) (v1.Layer, error) {
	buf, err := buildTarGz(entries)
	if err != nil {
		return nil, err
	}
	return tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf)), nil
	})
}

func buildTarGz(entries []Entry) ([]byte, error) {
	// Sort entries by destination path
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].DestinationPath < sorted[j].DestinationPath
	})

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Track created parent directories
	createdDirs := make(map[string]bool)

	for _, entry := range sorted {
		// Ensure parent directories exist
		if err := ensureParentDirs(tw, entry.DestinationPath, entry, createdDirs); err != nil {
			return nil, err
		}

		modTime := millisToTime(entry.ModificationTime)
		uid, gid := parseOwnership(entry.Ownership)

		fi, err := os.Stat(entry.SourcePath)
		if err != nil {
			return nil, err
		}

		if fi.IsDir() {
			destPath := strings.TrimSuffix(entry.DestinationPath, "/") + "/"
			hdr := &tar.Header{
				Typeflag: tar.TypeDir,
				Name:     destPath,
				Mode:     int64(entry.Permissions),
				ModTime:  modTime,
				Uid:      uid,
				Gid:      gid,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return nil, err
			}
			createdDirs[destPath] = true
		} else {
			f, err := os.Open(entry.SourcePath)
			if err != nil {
				return nil, err
			}

			hdr := &tar.Header{
				Typeflag: tar.TypeReg,
				Name:     entry.DestinationPath,
				Size:     fi.Size(),
				Mode:     int64(entry.Permissions),
				ModTime:  modTime,
				Uid:      uid,
				Gid:      gid,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				_ = f.Close()
				return nil, err
			}
			if _, err := io.Copy(tw, f); err != nil {
				_ = f.Close()
				return nil, err
			}
			_ = f.Close()
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func ensureParentDirs(tw *tar.Writer, destPath string, entry Entry, created map[string]bool) error {
	dir := path.Dir(destPath)
	if dir == "/" || dir == "." {
		return nil
	}

	// Collect all parent directories
	var dirs []string
	for d := dir; d != "/" && d != "."; d = path.Dir(d) {
		dirPath := d + "/"
		if created[dirPath] {
			break
		}
		dirs = append(dirs, dirPath)
	}

	// Create them in order (top-down)
	modTime := millisToTime(entry.ModificationTime)
	uid, gid := parseOwnership(entry.Ownership)
	for i := len(dirs) - 1; i >= 0; i-- {
		d := dirs[i]
		if created[d] {
			continue
		}
		hdr := &tar.Header{
			Typeflag: tar.TypeDir,
			Name:     d,
			Mode:     0755,
			ModTime:  modTime,
			Uid:      uid,
			Gid:      gid,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		created[d] = true
	}
	return nil
}

func millisToTime(millis int64) time.Time {
	return time.Unix(millis/1000, (millis%1000)*int64(time.Millisecond)).UTC()
}

func parseOwnership(ownership string) (int, int) {
	if ownership == "" {
		return 0, 0
	}
	parts := strings.SplitN(ownership, ":", 2)
	uid := 0
	gid := 0
	if len(parts) >= 1 {
		for _, c := range parts[0] {
			if c >= '0' && c <= '9' {
				uid = uid*10 + int(c-'0')
			}
		}
	}
	if len(parts) >= 2 {
		for _, c := range parts[1] {
			if c >= '0' && c <= '9' {
				gid = gid*10 + int(c-'0')
			}
		}
	}
	return uid, gid
}
