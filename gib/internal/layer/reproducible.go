// Package layer builds an image layer the way Jib lays one out, as a
// standard POSIX tar: an entry for each file and directory, every
// directory above them owned by root, sorted by name, the same bytes
// for the same entries every time.
package layer

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strconv"
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
	ModificationTime time.Time
	Ownership        string // "<user>:<group>", each a number or a name
}

// parentTime is when a directory the layer adds above its entries was
// modified: Jib's default, a second past the epoch.
var parentTime = time.Unix(1, 0).UTC()

// BuildReproducibleLayer creates a v1.Layer from file entries.
func BuildReproducibleLayer(entries []Entry) (v1.Layer, error) {
	raw, err := Tar(entries)
	if err != nil {
		return nil, err
	}
	return tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(raw)), nil
	})
}

// Tar is the uncompressed layer. Each name is added once, the first
// entry for it winning; before each entry, every directory above it not
// yet added, owned by root; and the entries are written sorted by name.
func Tar(entries []Entry) ([]byte, error) {
	var hs []*tar.Header
	sources := map[string]string{}
	added := map[string]bool{}
	var add func(h *tar.Header)
	add = func(h *tar.Header) {
		if added[h.Name] {
			return
		}
		if parent := path.Dir(strings.TrimSuffix(h.Name, "/")); parent != "." {
			add(&tar.Header{Typeflag: tar.TypeDir, Name: parent + "/", Mode: dirBits | 0o755, ModTime: parentTime, Format: tar.FormatPAX})
		}
		hs = append(hs, h)
		added[h.Name] = true
	}
	for _, e := range entries {
		h, err := header(e)
		if err != nil {
			return nil, err
		}
		if _, ok := sources[h.Name]; !ok && h.Typeflag == tar.TypeReg {
			sources[h.Name] = e.SourcePath
		}
		add(h)
	}
	sort.Slice(hs, func(i, j int) bool { return hs[i].Name < hs[j].Name })

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, h := range hs {
		if err := tw.WriteHeader(h); err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		f, err := os.Open(sources[h.Name])
		if err != nil {
			return nil, err
		}
		n, err := io.Copy(tw, f)
		_ = f.Close()
		if err != nil {
			return nil, err
		}
		if n != h.Size {
			return nil, fmt.Errorf("%s changed size while it was added to a layer", sources[h.Name])
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// The file type bits a tar mode carries, as Jib writes them.
const (
	dirBits  = 0o40000
	fileBits = 0o100000
)

// header is the tar header of an entry: named relative to the root, a
// directory's name ending in a slash, the source followed through a
// symbolic link, and anything neither a file nor a directory refused.
func header(e Entry) (*tar.Header, error) {
	fi, err := os.Stat(e.SourcePath)
	if err != nil {
		return nil, err
	}
	h := &tar.Header{
		Name:    strings.TrimLeft(path.Clean("/"+e.DestinationPath), "/"),
		ModTime: e.ModificationTime.UTC(),
		Format:  tar.FormatPAX,
	}
	switch {
	case fi.IsDir():
		h.Typeflag, h.Mode = tar.TypeDir, dirBits|int64(e.Permissions.Perm())
		h.Name += "/"
	case fi.Mode().IsRegular():
		h.Typeflag, h.Mode, h.Size = tar.TypeReg, fileBits|int64(e.Permissions.Perm()), fi.Size()
	default:
		return nil, fmt.Errorf("cannot add %s to a layer: it is neither a file nor a directory", e.SourcePath)
	}
	owner(h, e.Ownership)
	return h, nil
}

// owner sets ownership as Jib reads "<user>:<group>": a number is the
// id, and anything else the name, over id 0.
func owner(h *tar.Header, ownership string) {
	user, group, _ := strings.Cut(ownership, ":")
	if n, err := strconv.Atoi(user); err == nil {
		h.Uid = n
	} else {
		h.Uname = user
	}
	if n, err := strconv.Atoi(group); err == nil {
		h.Gid = n
	} else {
		h.Gname = group
	}
}
