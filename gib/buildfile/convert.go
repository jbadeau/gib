package buildfile

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/jbadeau/gib"
)

// ConvertOptions carries authentication and registry options for the base image.
type ConvertOptions struct {
	FromUsername            string
	FromPassword            string
	FromCredentialHelper    string
	AllowInsecureRegistries bool
}

// Convert transforms a BuildFileSpec into a ContainerBuilder as Jib's
// BuildFiles.toJibContainerBuilder does.
func Convert(spec *BuildFileSpec, contextDir string, opts *ConvertOptions) (*gib.ContainerBuilder, error) {
	if err := spec.check(); err != nil {
		return nil, err
	}
	builder, err := baseImageBuilder(spec.From, opts)
	if err != nil {
		return nil, err
	}
	if spec.CreationTime != "" {
		t, err := instant(spec.CreationTime, "creationTime")
		if err != nil {
			return nil, err
		}
		builder.SetCreationTime(t)
	}
	if spec.Format != "" {
		f, err := gib.ParseImageFormat(spec.Format)
		if err != nil {
			return nil, err
		}
		builder.SetFormat(f)
	}
	builder.SetEnvironment(spec.Environment)
	builder.SetLabels(spec.Labels)
	for _, v := range spec.Volumes {
		p, err := absoluteUnixPath(v)
		if err != nil {
			return nil, err
		}
		builder.AddVolume(p)
	}
	for _, s := range spec.ExposedPorts {
		ports, err := gib.ParsePorts(s)
		if err != nil {
			return nil, err
		}
		for _, p := range ports {
			builder.AddExposedPort(p)
		}
	}
	if spec.User != "" {
		builder.SetUser(spec.User)
	}
	if spec.WorkingDirectory != "" {
		p, err := absoluteUnixPath(spec.WorkingDirectory)
		if err != nil {
			return nil, err
		}
		builder.SetWorkingDirectory(p)
	}
	if spec.Entrypoint != nil {
		builder.SetEntrypoint(spec.Entrypoint...)
	}
	if spec.Cmd != nil {
		builder.SetProgramArguments(spec.Cmd...)
	}
	if spec.Layers == nil {
		return builder, nil
	}

	base := defaultProperties()
	if spec.Layers.Properties != nil {
		if base, err = mergeProperties(base, spec.Layers.Properties); err != nil {
			return nil, err
		}
	}
	for _, entry := range spec.Layers.Entries {
		if entry.Archive != "" {
			return nil, fmt.Errorf("Only FileLayers are supported at this time.") //nolint:staticcheck // Jib's message
		}
		props := base
		if entry.Properties != nil {
			if props, err = mergeProperties(props, entry.Properties); err != nil {
				return nil, err
			}
		}
		layer, err := buildLayer(entry, props, contextDir)
		if err != nil {
			return nil, fmt.Errorf("building layer %q: %w", entry.Name, err)
		}
		builder.AddFileEntriesLayer(layer)
	}
	return builder, nil
}

// baseImageBuilder is a builder over the base image as Jib's
// ContainerBuilders.create makes it: docker:// an image of the Docker
// daemon, tar:// an image tarball, its path relative to the working
// directory, and anything else, after an optional registry://, an image
// reference, "scratch" being none. Only a reference is built for the
// platforms named; Jib gives the others none.
func baseImageBuilder(from *BaseImageSpec, opts *ConvertOptions) (*gib.ContainerBuilder, error) {
	if from == nil {
		return gib.FromScratch(), nil
	}
	image := from.Image
	if ref, ok := strings.CutPrefix(image, "docker://"); ok {
		if _, err := name.ParseReference(ref); err != nil {
			return nil, fmt.Errorf("Invalid image reference: %s", ref) //nolint:staticcheck // Jib's message
		}
		return gib.FromImage(gib.DockerDaemonSource(ref)), nil
	}
	if p, ok := strings.CutPrefix(image, "tar://"); ok {
		var platforms []gib.Platform
		for _, pl := range from.Platforms {
			platforms = append(platforms, gib.Platform{Architecture: pl.Architecture, OS: pl.OS})
		}
		return gib.FromImage(gib.TarSource(p, gib.WithIndexPlatforms(platforms...))), nil
	}
	ref := strings.TrimPrefix(image, "registry://")
	var builder *gib.ContainerBuilder
	if ref == "scratch" {
		builder = gib.FromScratch()
	} else {
		if _, err := name.ParseReference(ref); err != nil {
			return nil, fmt.Errorf("Invalid image reference: %s", ref) //nolint:staticcheck // Jib's message
		}
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
		builder = gib.From(ref, sourceOpts...)
	}
	for _, p := range from.Platforms {
		builder.AddPlatform(p.Architecture, p.OS)
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
			var err error
			if props, err = mergeProperties(props, copySpec.Properties); err != nil {
				return l, err
			}
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
		dest, err := absoluteUnixPath(copySpec.Dest)
		if err != nil {
			return l, err
		}
		dest = path.Clean(dest)
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
	timestamp            time.Time
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

// defaultProperties are Jib's: 644 files, 755 directories, a second
// past the epoch.
func defaultProperties() resolvedProperties {
	return resolvedProperties{
		filePermissions:      0644,
		directoryPermissions: 0755,
		timestamp:            time.Unix(1, 0).UTC(),
	}
}

// mergeProperties sets over base what override sets.
func mergeProperties(base resolvedProperties, override *FilePropertiesSpec) (resolvedProperties, error) {
	for _, m := range []string{override.FilePermissions, override.DirectoryPermissions} {
		if m != "" {
			if err := permissions(&m); err != nil {
				return base, err
			}
		}
	}
	if override.FilePermissions != "" {
		perm, _ := strconv.ParseUint(override.FilePermissions, 8, 32)
		base.filePermissions = fs.FileMode(perm)
	}
	if override.DirectoryPermissions != "" {
		perm, _ := strconv.ParseUint(override.DirectoryPermissions, 8, 32)
		base.directoryPermissions = fs.FileMode(perm)
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
		t, err := instant(override.Timestamp, "timestamp")
		if err != nil {
			return base, err
		}
		base.timestamp = t
	}
	return base, nil
}
