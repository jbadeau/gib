// Package layer builds an image layer exactly as Jib does: the same
// entries, in the same order, with the same tar headers, byte for byte.
// What Jib writes is decided by its ReproducibleLayerBuilder and by the
// TarArchiveOutputStream of Apache Commons Compress it writes with; the
// tar here is theirs, ported.
package layer

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"unicode/utf16"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// Entry represents a single file to add to a layer.
type Entry struct {
	SourcePath       string
	DestinationPath  string
	Permissions      fs.FileMode
	ModificationTime int64  // millis since epoch
	Ownership        string // "<user>:<group>", each a number or a name
}

// BuildReproducibleLayer creates a v1.Layer from file entries, its tar
// the bytes Jib writes for them.
func BuildReproducibleLayer(entries []Entry) (v1.Layer, error) {
	raw, err := Tar(entries)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(raw); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	gz := buf.Bytes()
	return tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(gz)), nil
	})
}

// Tar is the uncompressed layer. As Jib's builder does, it adds each
// entry once, the first of a name winning; before each, every directory
// above it not yet added, owned by root and dated a second past the
// epoch; and it writes them sorted by name.
func Tar(entries []Entry) ([]byte, error) {
	u := unique{names: map[string]bool{}}
	for _, e := range entries {
		t, err := fromEntry(e)
		if err != nil {
			return nil, err
		}
		u.add(t)
	}
	sortEntries(u.entries)
	var buf bytes.Buffer
	for _, t := range u.entries {
		if err := t.write(&buf); err != nil {
			return nil, err
		}
	}
	buf.Write(make([]byte, 2*record))
	return buf.Bytes(), nil
}

// tarEntry is what Commons Compress's TarArchiveEntry holds for an
// entry Jib writes.
type tarEntry struct {
	name         string // relative; a directory's ends in "/"
	dir          bool
	mode         int64
	uid, gid     int64
	uname, gname string
	secs         int64 // the time, in seconds and nanoseconds
	nanos        int64
	size         int64
	source       string
}

const (
	record      = 512
	dirMode     = 0o40755
	fileMode    = 0o100644
	maxID       = 0o7777777
	maxSize     = 0o77777777777
	nameLen     = 100
	defaultSecs = 1 // FileEntriesLayer.DEFAULT_MODIFICATION_TIME
)

func fromEntry(e Entry) (*tarEntry, error) {
	fi, err := os.Stat(e.SourcePath)
	if err != nil {
		return nil, err
	}
	t := &tarEntry{name: normalize(e.DestinationPath), source: e.SourcePath}
	if fi.IsDir() {
		t.dir, t.mode = true, dirMode
		if !strings.HasSuffix(t.name, "/") {
			t.name += "/"
		}
	} else {
		if !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("cannot add %s to a layer: it is neither a file nor a directory", e.SourcePath)
		}
		t.mode, t.size = fileMode, fi.Size()
	}
	t.mode = t.mode&^0o777 | int64(e.Permissions.Perm())
	t.owner(e.Ownership)
	t.secs, t.nanos = jibTime(e.ModificationTime)
	return t, nil
}

// normalize is the name Commons Compress gives a path: without leading
// slashes, and without the trailing one Jib's extraction path never has.
func normalize(p string) string {
	p = path.Clean("/" + p)
	return strings.TrimLeft(p, "/")
}

// owner sets ownership as Jib parses "<user>:<group>": each a number,
// which is the id, or a name, which is the name over id 0.
func (t *tarEntry) owner(ownership string) {
	user, group, _ := strings.Cut(ownership, ":")
	set := func(v string, id *int64, name *string) {
		if v == "" {
			return
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			*id = n
		} else {
			*name = v
		}
	}
	set(user, &t.uid, &t.uname)
	set(group, &t.gid, &t.gname)
}

// jibTime is the time Jib gives an entry modified at millis. Jib writes
// it as seconds and its nanoseconds unpadded, "1.50000000" for 1050ms,
// and Commons Compress reads that back as a decimal, 1.5: the time
// stored is that one.
func jibTime(millis int64) (int64, int64) {
	secs, nanos := floorDiv(millis, 1000), floorMod(millis, 1000)*1_000_000
	if nanos == 0 {
		return secs, 0
	}
	digits := strconv.FormatInt(nanos, 10)
	for len(digits) < 9 {
		digits += "0"
	}
	n, _ := strconv.ParseInt(digits[:9], 10, 64)
	return secs, n
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func floorMod(a, b int64) int64 { return a - floorDiv(a, b)*b }

// unique is Jib's UniqueTarArchiveEntries.
type unique struct {
	entries []*tarEntry
	names   map[string]bool
}

func (u *unique) add(t *tarEntry) {
	if u.names[t.name] {
		return
	}
	if parent := path.Dir(strings.TrimSuffix(t.name, "/")); parent != "." && parent != "/" {
		u.add(&tarEntry{name: parent + "/", dir: true, mode: dirMode, secs: defaultSecs})
	}
	u.entries = append(u.entries, t)
	u.names[t.name] = true
}

// sortEntries sorts by name as Java compares strings: by UTF-16 code
// unit, stably.
func sortEntries(es []*tarEntry) {
	key := func(s string) []uint16 { return utf16.Encode([]rune(s)) }
	for i := 1; i < len(es); i++ {
		for j := i; j > 0 && less(key(es[j].name), key(es[j-1].name)); j-- {
			es[j], es[j-1] = es[j-1], es[j]
		}
	}
}

func less(a, b []uint16) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// write writes the entry as TarArchiveOutputStream writes it, with
// BIGNUMBER_POSIX and LONGFILE_POSIX: a PAX header of what the ustar
// header cannot hold or holds coarser, then the header, then the
// content.
func (t *tarEntry) write(w *bytes.Buffer) error {
	pax := &javaMap{}
	if len(t.name) >= nameLen {
		pax.put("path", t.name)
	}
	if t.size > maxSize {
		pax.put("size", strconv.FormatInt(t.size, 10))
	}
	if t.gid < 0 || t.gid > maxID {
		pax.put("gid", strconv.FormatInt(t.gid, 10))
	}
	if t.nanos != 0 || t.secs < 0 || t.secs > maxSize {
		pax.put("mtime", paxTime(t.secs, t.nanos))
	}
	pax.put("atime", paxTime(t.secs, t.nanos))
	pax.put("ctime", paxTime(t.secs, t.nanos))
	if t.uid < 0 || t.uid > maxID {
		pax.put("uid", strconv.FormatInt(t.uid, 10))
	}
	pax.put("LIBARCHIVE.creationtime", paxTime(t.secs, t.nanos))

	data := pax.encode()
	paxName := "./PaxHeaders.X/" + strip7(t.name)
	if len(paxName) >= nameLen {
		paxName = paxName[:nameLen-1]
	}
	mtime := t.secs
	if mtime < 0 || mtime > maxSize {
		mtime = 0
	}
	writeHeader(w, header{name: paxName, mode: fileMode, size: int64(len(data)), mtime: mtime, typeflag: 'x'})
	writeData(w, data)

	size := t.size
	if t.dir {
		size = 0
	}
	typeflag := byte('0')
	if t.dir {
		typeflag = '5'
	}
	writeHeader(w, header{name: t.name, mode: t.mode, uid: t.uid, gid: t.gid, size: size, mtime: t.secs,
		typeflag: typeflag, uname: t.uname, gname: t.gname})
	if t.dir {
		return nil
	}
	content, err := os.ReadFile(t.source)
	if err != nil {
		return err
	}
	if int64(len(content)) != t.size {
		return fmt.Errorf("%s changed size while it was added to a layer", t.source)
	}
	writeData(w, content)
	return nil
}

// paxTime is a time as a PAX record holds it: whole seconds, or seconds
// with seven decimals, truncated.
func paxTime(secs, nanos int64) string {
	if nanos == 0 {
		return strconv.FormatInt(secs, 10)
	}
	return fmt.Sprintf("%d.%07d", secs, nanos/100)
}

// strip7 is the entry name as a PAX header's name holds it: 7 bits a
// character, and '/', '\' and NUL replaced by '_'.
func strip7(s string) string {
	var b strings.Builder
	for _, u := range utf16.Encode([]rune(s)) {
		c := byte(u & 0x7f)
		if c == 0 || c == '/' || c == '\\' {
			c = '_'
		}
		b.WriteByte(c)
	}
	return b.String()
}

type header struct {
	name         string
	mode         int64
	uid, gid     int64
	size         int64
	mtime        int64
	typeflag     byte
	uname, gname string
}

// writeHeader writes a ustar header record as TarArchiveEntry's
// writeEntryHeader does: numbers in zero-padded octal and a space, a
// number too big for its field written as 0 (its PAX record holds it),
// the checksum six octal digits, a NUL and a space.
func writeHeader(w *bytes.Buffer, h header) {
	var b [record]byte
	o := 0
	name := func(s string, n int) {
		copy(b[o:o+n], truncate(s, n))
		o += n
	}
	num := func(v int64, n int) {
		if v < 0 || v >= 1<<(3*(n-1)) {
			v = 0
		}
		s := strconv.FormatInt(v, 8)
		for len(s) < n-1 {
			s = "0" + s
		}
		copy(b[o:], s)
		b[o+n-1] = ' '
		o += n
	}
	name(h.name, 100)
	num(h.mode, 8)
	num(h.uid, 8)
	num(h.gid, 8)
	num(h.size, 12)
	num(h.mtime, 12)
	csOff := o
	for i := range 8 {
		b[o+i] = ' '
	}
	o += 8
	b[o] = h.typeflag
	o++
	name("", 100) // link name
	name("ustar\x00", 6)
	name("00", 2)
	name(h.uname, 32)
	name(h.gname, 32)
	num(0, 8) // device major
	num(0, 8) // device minor
	var sum int64
	for _, c := range b {
		sum += int64(c)
	}
	cs := strconv.FormatInt(sum, 8)
	for len(cs) < 6 {
		cs = "0" + cs
	}
	copy(b[csOff:], cs)
	b[csOff+6] = 0
	b[csOff+7] = ' '
	w.Write(b[:])
}

// truncate is s in UTF-8, shortened a character at a time to fit n
// bytes, as TarUtils.formatNameBytes does.
func truncate(s string, n int) []byte {
	r := []rune(s)
	for len(string(r)) > n && len(r) > 0 {
		r = r[:len(r)-1]
	}
	return []byte(string(r))
}

// writeData writes content padded with zeros to a whole record.
func writeData(w *bytes.Buffer, data []byte) {
	w.Write(data)
	if pad := len(data) % record; pad != 0 {
		w.Write(make([]byte, record-pad))
	}
}

// javaMap holds PAX records in the order Java's HashMap, which Commons
// Compress collects them in, iterates them: by bucket of a 16-bucket
// table, each bucket in insertion order. Fewer than 13 records never
// grow the table.
type javaMap struct {
	keys   []string
	values map[string]string
}

func (m *javaMap) put(k, v string) {
	if m.values == nil {
		m.values = map[string]string{}
	}
	if _, ok := m.values[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.values[k] = v
}

func (m *javaMap) encode() []byte {
	var out bytes.Buffer
	for bucket := range uint32(16) {
		for _, k := range m.keys {
			if javaBucket(k) != bucket {
				continue
			}
			v := m.values[k]
			l := len(k) + len(v) + 3 + 2
			line := strconv.Itoa(l) + " " + k + "=" + v + "\n"
			for l != len(line) {
				l = len(line)
				line = strconv.Itoa(l) + " " + k + "=" + v + "\n"
			}
			out.WriteString(line)
		}
	}
	return out.Bytes()
}

// javaBucket is the bucket of a 16-bucket HashMap a String key falls in:
// String.hashCode, spread as HashMap.hash spreads it.
func javaBucket(s string) uint32 {
	var h int32
	for _, u := range utf16.Encode([]rune(s)) {
		h = 31*h + int32(u)
	}
	x := uint32(h)
	return (x ^ x>>16) & 15
}
