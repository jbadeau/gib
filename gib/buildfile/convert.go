package buildfile

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jbadeau/gib"
)

// ConvertOptions carries authentication and registry options for the base image.
type ConvertOptions struct {
	FromUsername            string
	FromPassword            string
	FromCredentialHelper    string
	AllowInsecureRegistries bool
}

// Convert transforms a BuildFileSpec into a ContainerBuilder.
func Convert(spec *BuildFileSpec, contextDir string, opts *ConvertOptions) (*gib.ContainerBuilder, error) {
	var builder *gib.ContainerBuilder

	// Base image. Jib's from.image accepts scheme prefixes — registry://
	// (the default), tar:// (an image tarball on disk), docker:// (daemon)
	// — plus the literal "scratch". gib matches that, minus docker://,
	// which a daemonless builder cannot support. A relative tar path is
	// resolved against the context directory, like layer sources.
	switch {
	case spec.From == nil || spec.From.Image == "" || spec.From.Image == "scratch":
		builder = gib.FromScratch()
	case strings.HasPrefix(spec.From.Image, "tar://"):
		tarPath := strings.TrimPrefix(spec.From.Image, "tar://")
		if !filepath.IsAbs(tarPath) {
			tarPath = filepath.Join(contextDir, tarPath)
		}
		builder = gib.FromImage(gib.TarSource(tarPath))
	case strings.HasPrefix(spec.From.Image, "docker://"):
		return nil, fmt.Errorf("from.image %q: docker:// bases need a Docker daemon, which gib does not use; export the image to a tarball and use tar:// instead", spec.From.Image)
	default:
		var sourceOpts []gib.ImageSourceOption
		if opts != nil {
			if opts.FromUsername != "" && opts.FromPassword != "" {
				sourceOpts = append(sourceOpts, gib.WithSourceCredentials(opts.FromUsername, opts.FromPassword))
			} else if opts.FromCredentialHelper != "" {
				sourceOpts = append(sourceOpts, gib.WithSourceCredentialHelper(opts.FromCredentialHelper))
			}
			if opts.AllowInsecureRegistries {
				sourceOpts = append(sourceOpts, gib.WithSourceInsecure())
			}
		}
		builder = gib.From(strings.TrimPrefix(spec.From.Image, "registry://"), sourceOpts...)
	}

	// Platforms
	if spec.From != nil {
		for _, p := range spec.From.Platforms {
			builder.AddPlatform(p.Architecture, p.OS)
		}
	}

	// Creation time
	if spec.CreationTime != "" {
		millis, err := parseTimestamp(spec.CreationTime)
		if err != nil {
			return nil, fmt.Errorf("invalid creationTime: %w", err)
		}
		builder.SetCreationTime(millis)
	}

	// Format
	if spec.Format != "" {
		builder.SetFormat(gib.ParseImageFormat(spec.Format))
	}

	// Environment
	if len(spec.Environment) > 0 {
		builder.SetEnvironment(spec.Environment)
	}

	// Labels
	if len(spec.Labels) > 0 {
		builder.SetLabels(spec.Labels)
	}

	// Volumes
	for _, v := range spec.Volumes {
		builder.AddVolume(v)
	}

	// Exposed ports
	for _, portStr := range spec.ExposedPorts {
		p, err := gib.ParsePort(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid port %q: %w", portStr, err)
		}
		builder.AddExposedPort(p)
	}

	// User
	if spec.User != "" {
		builder.SetUser(spec.User)
	}

	// Working directory
	if spec.WorkingDirectory != "" {
		builder.SetWorkingDirectory(spec.WorkingDirectory)
	}

	// Entrypoint
	if spec.Entrypoint != nil {
		builder.SetEntrypoint(spec.Entrypoint...)
	}

	// Cmd
	if spec.Cmd != nil {
		builder.SetProgramArguments(spec.Cmd...)
	}

	// Layers
	if spec.Layers != nil {
		globalProps := defaultProperties()
		if spec.Layers.Properties != nil {
			globalProps = mergeProperties(globalProps, spec.Layers.Properties)
		}

		for _, entry := range spec.Layers.Entries {
			layerProps := globalProps
			if entry.Properties != nil {
				layerProps = mergeProperties(layerProps, entry.Properties)
			}

			layer, err := buildLayer(entry, layerProps, contextDir)
			if err != nil {
				return nil, fmt.Errorf("building layer %q: %w", entry.Name, err)
			}
			builder.AddFileEntriesLayer(layer)
		}
	}

	return builder, nil
}

// buildLayer makes a layer of a layer spec as Jib's Layers.toLayers
// does: paths spelled as Java's Path spells them, a directory walked in
// the order its file system lists it, and each entry carrying the
// properties of the copy that adds it.
func buildLayer(entry LayerEntrySpec, layerProps resolvedProperties, contextDir string) (gib.FileEntriesLayer, error) {
	l := gib.FileEntriesLayer{Name: entry.Name}
	for _, copySpec := range entry.Files {
		props := layerProps
		if copySpec.Properties != nil {
			props = mergeProperties(props, copySpec.Properties)
		}
		add := func(src, dest string, perm fs.FileMode) {
			l.Entries = append(l.Entries, gib.FileEntry{
				SourcePath:       src,
				DestinationPath:  dest,
				Permissions:      perm,
				ModificationTime: props.timestamp,
				Ownership:        props.ownership(),
			})
		}
		rawSrc := javaPath(copySpec.Src)
		src := rawSrc
		if !strings.HasPrefix(rawSrc, "/") {
			src = javaResolve(javaPath(contextDir), rawSrc)
		}
		dest := path.Clean("/" + copySpec.Dest)
		notFile := fmt.Errorf("cannot create FileLayers from non-file, non-directory: %s", src)
		fi, err := os.Stat(src)
		switch {
		case err != nil || !fi.IsDir() && !fi.Mode().IsRegular():
			return l, notFile
		case !fi.IsDir():
			if len(copySpec.Includes)+len(copySpec.Excludes) > 0 {
				return l, fmt.Errorf("cannot apply includes/excludes on single file copy directives")
			}
			if strings.HasSuffix(copySpec.Dest, "/") {
				dest = path.Join(dest, path.Base(src))
			}
			add(src, dest, props.filePermissions)
			continue
		}

		includes, err := matchers(copySpec.Includes)
		if err != nil {
			return l, err
		}
		excludes, err := matchers(copySpec.Excludes)
		if err != nil {
			return l, err
		}
		paths, err := walk(src)
		if err != nil {
			return l, err
		}
		target := func(p string) string {
			if p == src {
				return dest
			}
			return path.Join(dest, strings.TrimPrefix(p, src+"/"))
		}
		added := map[string]bool{}
		for _, p := range paths {
			if anyMatch(excludes, p) || len(includes) > 0 && !anyMatch(includes, p) {
				continue
			}
			fi, err := os.Stat(p)
			if err != nil || !fi.IsDir() && !fi.Mode().IsRegular() {
				return l, notFile
			}
			if fi.IsDir() {
				added[p] = true
				add(p, target(p), props.directoryPermissions)
				continue
			}
			// A file brings every directory between it and the source
			// not yet added, nearest first.
			for parent := javaParent(p); !added[parent]; parent = javaParent(parent) {
				add(parent, target(parent), props.directoryPermissions)
				added[parent] = true
				if parent == src {
					break
				}
			}
			add(p, target(p), props.filePermissions)
		}
	}
	return l, nil
}

func matchers(globs []string) ([]pathMatcher, error) {
	var out []pathMatcher
	for _, g := range globs {
		m, err := newPathMatcher(g)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func anyMatch(ms []pathMatcher, p string) bool {
	for _, m := range ms {
		if m.matches(p) {
			return true
		}
	}
	return false
}

// walk lists root and everything beneath it as Java's Files.walk does:
// depth first, a directory before what it holds, and a symbolic link
// listed but not followed. Each directory's entries come sorted by name,
// one of the orders Files.walk may list them in and the same on every
// file system, so the same files are always the same layer.
func walk(root string) ([]string, error) {
	out := []string{root}
	fi, err := os.Lstat(root)
	if err != nil || !fi.IsDir() {
		return out, err
	}
	var visit func(dir string) error
	visit = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			p := javaResolve(dir, e.Name())
			out = append(out, p)
			if e.IsDir() {
				if err := visit(p); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return out, visit(root)
}

// javaPath is a path as Java's Paths.get spells it: repeated slashes
// collapsed and a trailing one dropped, "." and ".." kept.
func javaPath(p string) string {
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// javaResolve is base.resolve(other) for relative other.
func javaResolve(base, other string) string {
	switch {
	case other == "":
		return base
	case base == "":
		return other
	case base == "/":
		return "/" + other
	}
	return base + "/" + other
}

// javaParent is p.getParent().
func javaParent(p string) string {
	i := strings.LastIndex(p, "/")
	switch {
	case i > 0:
		return p[:i]
	case i == 0:
		return "/"
	}
	return ""
}

type resolvedProperties struct {
	filePermissions      fs.FileMode
	directoryPermissions fs.FileMode
	user                 *string
	group                *string
	timestamp            int64
}

// ownership is how Jib's FilePropertiesStack spells it: the user, then
// ":" and the group when there is one.
func (p resolvedProperties) ownership() string {
	var s string
	if p.user != nil {
		s = *p.user
	}
	if p.group != nil {
		s += ":" + *p.group
	}
	return s
}

func defaultProperties() resolvedProperties {
	return resolvedProperties{
		filePermissions:      0644,
		directoryPermissions: 0755,
		timestamp:            1000, // epoch + 1s in millis
	}
}

func mergeProperties(base resolvedProperties, override *FilePropertiesSpec) resolvedProperties {
	if override.FilePermissions != "" {
		if perm, err := strconv.ParseUint(override.FilePermissions, 8, 32); err == nil {
			base.filePermissions = fs.FileMode(perm)
		}
	}
	if override.DirectoryPermissions != "" {
		if perm, err := strconv.ParseUint(override.DirectoryPermissions, 8, 32); err == nil {
			base.directoryPermissions = fs.FileMode(perm)
		}
	}
	if override.User != "" {
		u := override.User
		base.user = &u
	}
	if override.Group != "" {
		g := override.Group
		base.group = &g
	}
	if override.Timestamp != "" {
		if ts, err := parseTimestamp(override.Timestamp); err == nil {
			base.timestamp = ts
		}
	}
	return base
}

// parseTimestamp parses a timestamp as millis or ISO 8601.
func parseTimestamp(s string) (int64, error) {
	// Try as millis first
	if millis, err := strconv.ParseInt(s, 10, 64); err == nil {
		return millis, nil
	}

	// Try as ISO 8601
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t.UnixMilli(), nil
		}
	}

	return 0, fmt.Errorf("cannot parse timestamp %q: expected millis or ISO 8601", s)
}
