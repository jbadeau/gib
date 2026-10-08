package buildfile

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/jbadeau/gib"
	"gopkg.in/yaml.v3"
)

// Parse reads and parses a build file from the given path.
func Parse(path string, params map[string]string) (*BuildFileSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading build file: %w", err)
	}
	return ParseBytes(data, params)
}

// ParseBytes parses a build file as Jib's BuildFiles does: its template
// parameters substituted, its YAML read as Jackson reads it into Jib's
// spec classes, and each refused where those classes' constructors
// refuse it.
func ParseBytes(data []byte, params map[string]string) (*BuildFileSpec, error) {
	text, err := substitute(string(data), params)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, fmt.Errorf("parsing build file YAML: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, errors.New("No content to map due to end-of-input") //nolint:staticcheck // Jackson's message
	}
	root := doc.Content[0]
	if isNull(root) {
		return nil, errors.New("the build file is null")
	}
	return buildFile(root)
}

// The checks of Jib's Validator.

func notNullNotEmpty(v *string, name string) error {
	if v == nil {
		return fmt.Errorf("Property '%s' cannot be null", name) //nolint:staticcheck // Jib's message
	}
	return nullOrNotEmpty(v, name)
}

func nullOrNotEmpty(v *string, name string) error {
	if v != nil && strings.TrimSpace(*v) == "" {
		return fmt.Errorf("Property '%s' cannot be an empty string", name) //nolint:staticcheck // Jib's message
	}
	return nil
}

func nonNullNonEmptyEntries(vs []*string, name string) error {
	for _, v := range vs {
		if v == nil {
			return fmt.Errorf("Property '%s' cannot contain null entries", name) //nolint:staticcheck // Jib's message
		}
		if strings.TrimSpace(*v) == "" {
			return fmt.Errorf("Property '%s' cannot contain empty strings", name) //nolint:staticcheck // Jib's message
		}
	}
	return nil
}

func nonNullNonEmptyMap(es []entry, name string) error {
	for _, e := range es {
		if strings.TrimSpace(e.key) == "" {
			return fmt.Errorf("Property '%s' cannot contain empty string keys", name) //nolint:staticcheck // Jib's message
		}
		if e.value == nil {
			return fmt.Errorf("Property '%s' cannot contain null values", name) //nolint:staticcheck // Jib's message
		}
		if strings.TrimSpace(*e.value) == "" {
			return fmt.Errorf("Property '%s' cannot contain empty string values", name) //nolint:staticcheck // Jib's message
		}
	}
	return nil
}

func setStr(p **string) func(*yaml.Node) error {
	return func(v *yaml.Node) (err error) { *p, err = str(v); return }
}

func setStrs(p *[]*string, has *bool) func(*yaml.Node) error {
	return func(v *yaml.Node) (err error) { *p, *has, err = strs(v); return }
}

func setMap(p *[]entry, has *bool) func(*yaml.Node) error {
	return func(v *yaml.Node) (err error) { *p, *has, err = strMap(v); return }
}

func val(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func vals(vs []*string, present bool) []string {
	if !present {
		return nil
	}
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = *v
	}
	return out
}

func valMap(es []entry, present bool) map[string]string {
	if !present {
		return nil
	}
	out := make(map[string]string, len(es))
	for _, e := range es {
		out[e.key] = *e.value
	}
	return out
}

func first(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// buildFile is BuildFileSpec's creator.
func buildFile(n *yaml.Node) (*BuildFileSpec, error) {
	var (
		apiVersion, kind, creationTime, format, user, workingDirectory *string
		env, labels                                                    []entry
		hasEnv, hasLabels                                              bool
		volumes, ports, entrypoint, cmd                                []*string
		hasVolumes, hasPorts, hasEntrypoint, hasCmd                    bool
		spec                                                           BuildFileSpec
	)
	_, err := object(n, "BuildFileSpec",
		prop{"apiVersion", true, setStr(&apiVersion)},
		prop{"kind", true, setStr(&kind)},
		prop{"from", false, func(v *yaml.Node) (err error) { spec.From, err = baseImage(v); return }},
		prop{"creationTime", false, setStr(&creationTime)},
		prop{"format", false, setStr(&format)},
		prop{"environment", false, setMap(&env, &hasEnv)},
		prop{"labels", false, setMap(&labels, &hasLabels)},
		prop{"volumes", false, setStrs(&volumes, &hasVolumes)},
		prop{"exposedPorts", false, setStrs(&ports, &hasPorts)},
		prop{"user", false, setStr(&user)},
		prop{"workingDirectory", false, setStr(&workingDirectory)},
		prop{"entrypoint", false, setStrs(&entrypoint, &hasEntrypoint)},
		prop{"cmd", false, setStrs(&cmd, &hasCmd)},
		prop{"layers", false, func(v *yaml.Node) (err error) { spec.Layers, err = layers(v); return }},
	)
	if err != nil {
		return nil, err
	}
	err = first(
		notNullNotEmpty(apiVersion, "apiVersion"),
		equals(kind, "kind", "BuildFile"),
		nullOrNotEmpty(creationTime, "creationTime"),
		nullOrNotEmpty(format, "format"),
		nonNullNonEmptyMap(env, "environment"),
		nonNullNonEmptyMap(labels, "labels"),
		nonNullNonEmptyEntries(volumes, "volumes"),
		nonNullNonEmptyEntries(ports, "exposedPorts"),
		nullOrNotEmpty(user, "user"),
		nullOrNotEmpty(workingDirectory, "workingDirectory"),
		nonNullNonEmptyEntries(entrypoint, "entrypoint"),
		nonNullNonEmptyEntries(cmd, "cmd"),
	)
	if err != nil {
		return nil, err
	}
	spec.APIVersion, spec.Kind = *apiVersion, *kind
	spec.CreationTime, spec.Format = val(creationTime), val(format)
	spec.Environment, spec.Labels = valMap(env, hasEnv), valMap(labels, hasLabels)
	spec.Volumes, spec.ExposedPorts = vals(volumes, hasVolumes), vals(ports, hasPorts)
	spec.User, spec.WorkingDirectory = val(user), val(workingDirectory)
	spec.Entrypoint, spec.Cmd = vals(entrypoint, hasEntrypoint), vals(cmd, hasCmd)
	if err := spec.check(); err != nil {
		return nil, err
	}
	return &spec, nil
}

// check is what BuildFileSpec's creator makes of its values, refusing
// those it cannot: the creation time, the format, the volumes, the
// ports and the working directory.
func (s *BuildFileSpec) check() error {
	if s.CreationTime != "" {
		if _, err := instant(s.CreationTime, "creationTime"); err != nil {
			return err
		}
	}
	if s.Format != "" {
		if _, err := gib.ParseImageFormat(s.Format); err != nil {
			return err
		}
	}
	for _, v := range s.Volumes {
		if _, err := absoluteUnixPath(v); err != nil {
			return err
		}
	}
	for _, p := range s.ExposedPorts {
		if _, err := gib.ParsePorts(p); err != nil {
			return err
		}
	}
	if s.WorkingDirectory != "" {
		if _, err := absoluteUnixPath(s.WorkingDirectory); err != nil {
			return err
		}
	}
	return nil
}

func equals(v *string, name, want string) error {
	if v == nil {
		return fmt.Errorf("Property '%s' cannot be null", name) //nolint:staticcheck // Jib's message
	}
	if *v != want {
		return fmt.Errorf("Property '%s' must be '%s' but is '%s'", name, want, *v) //nolint:staticcheck // Jib's message
	}
	return nil
}

// baseImage is BaseImageSpec's creator.
func baseImage(n *yaml.Node) (*BaseImageSpec, error) {
	var image *string
	var platforms []*PlatformSpec
	ok, err := object(n, "BaseImageSpec",
		prop{"image", true, setStr(&image)},
		prop{"platforms", false, func(v *yaml.Node) error {
			_, err := list(v, func(e *yaml.Node) error {
				if e == nil {
					platforms = append(platforms, nil)
					return nil
				}
				p, err := platform(e)
				platforms = append(platforms, p)
				return err
			})
			return err
		}},
	)
	if !ok || err != nil {
		return nil, err
	}
	if err := notNullNotEmpty(image, "image"); err != nil {
		return nil, err
	}
	b := &BaseImageSpec{Image: *image}
	for _, p := range platforms {
		if p == nil {
			return nil, errors.New("Property 'platforms' cannot contain null entries") //nolint:staticcheck // Jib's message
		}
		b.Platforms = append(b.Platforms, *p)
	}
	return b, nil
}

// platform is PlatformSpec's creator.
func platform(n *yaml.Node) (*PlatformSpec, error) {
	var arch, os *string
	_, err := object(n, "PlatformSpec",
		prop{"architecture", true, setStr(&arch)},
		prop{"os", true, setStr(&os)},
	)
	if err == nil {
		err = first(notNullNotEmpty(arch, "architecture"), notNullNotEmpty(os, "os"))
	}
	if err != nil {
		return nil, err
	}
	return &PlatformSpec{Architecture: *arch, OS: *os}, nil
}

// layers is LayersSpec's creator.
func layers(n *yaml.Node) (*LayersSpec, error) {
	var entries []LayerEntrySpec
	var hasEntries bool
	var props *FilePropertiesSpec
	ok, err := object(n, "LayersSpec",
		prop{"entries", true, func(v *yaml.Node) (err error) {
			entries = []LayerEntrySpec{}
			hasEntries, err = list(v, func(e *yaml.Node) error {
				if e == nil {
					return errors.New("a layer cannot be null")
				}
				l, err := layer(e)
				entries = append(entries, l)
				return err
			})
			return err
		}},
		prop{"properties", false, func(v *yaml.Node) (err error) { props, err = fileProperties(v); return }},
	)
	if !ok || err != nil {
		return nil, err
	}
	switch {
	case !hasEntries:
		return nil, errors.New("Property 'entries' cannot be null") //nolint:staticcheck // Jib's message
	case len(entries) == 0:
		return nil, errors.New("Property 'entries' cannot be an empty collection") //nolint:staticcheck // Jib's message
	}
	return &LayersSpec{Entries: entries, Properties: props}, nil
}

// layer is LayerSpec's deserializer: an archive layer if the entry has
// an archive, or else a file layer if it has files.
func layer(n *yaml.Node) (LayerEntrySpec, error) {
	switch {
	case !has(n, "name"):
		return LayerEntrySpec{}, errors.New("Could not parse layer entry, missing required property 'name'") //nolint:staticcheck // Jib's message
	case has(n, "archive"):
		return archiveLayer(n)
	case has(n, "files"):
		return fileLayer(n)
	}
	return LayerEntrySpec{}, errors.New("Could not parse entry into ArchiveLayer or FileLayer") //nolint:staticcheck // Jib's message
}

// archiveLayer is ArchiveLayerSpec's creator.
func archiveLayer(n *yaml.Node) (LayerEntrySpec, error) {
	var name, archive, mediaType *string
	_, err := object(n, "ArchiveLayerSpec",
		prop{"name", true, setStr(&name)},
		prop{"archive", true, setStr(&archive)},
		prop{"mediaType", false, setStr(&mediaType)},
	)
	if err == nil {
		err = first(notNullNotEmpty(name, "name"), notNullNotEmpty(archive, "archive"),
			nullOrNotEmpty(mediaType, "mediaType"), checkPath(val(archive)))
	}
	if err != nil {
		return LayerEntrySpec{}, err
	}
	return LayerEntrySpec{Name: *name, Archive: *archive, MediaType: val(mediaType)}, nil
}

// fileLayer is FileLayerSpec's creator.
func fileLayer(n *yaml.Node) (LayerEntrySpec, error) {
	var name *string
	var files []CopyDirective
	var hasFiles bool
	var props *FilePropertiesSpec
	_, err := object(n, "FileLayerSpec",
		prop{"name", true, setStr(&name)},
		prop{"files", true, func(v *yaml.Node) (err error) {
			files = []CopyDirective{}
			hasFiles, err = list(v, func(e *yaml.Node) error {
				if e == nil {
					return errors.New("a copy cannot be null")
				}
				c, err := copySpec(e)
				files = append(files, c)
				return err
			})
			return err
		}},
		prop{"properties", false, func(v *yaml.Node) (err error) { props, err = fileProperties(v); return }},
	)
	if err == nil {
		err = notNullNotEmpty(name, "name")
	}
	if err == nil && !hasFiles {
		err = errors.New("Property 'files' cannot be null") //nolint:staticcheck // Jib's message
	}
	if err == nil && len(files) == 0 {
		err = errors.New("Property 'files' cannot be an empty collection") //nolint:staticcheck // Jib's message
	}
	if err != nil {
		return LayerEntrySpec{}, err
	}
	return LayerEntrySpec{Name: *name, Files: files, Properties: props}, nil
}

// copySpec is CopySpec's creator.
func copySpec(n *yaml.Node) (CopyDirective, error) {
	var src, dest *string
	var includes, excludes []*string
	var ignored bool
	var props *FilePropertiesSpec
	_, err := object(n, "CopySpec",
		prop{"src", true, setStr(&src)},
		prop{"dest", true, setStr(&dest)},
		prop{"includes", false, setStrs(&includes, &ignored)},
		prop{"excludes", false, setStrs(&excludes, &ignored)},
		prop{"properties", false, func(v *yaml.Node) (err error) { props, err = fileProperties(v); return }},
	)
	if err == nil {
		err = first(notNullNotEmpty(src, "src"), notNullNotEmpty(dest, "dest"),
			nonNullNonEmptyEntries(includes, "includes"), nonNullNonEmptyEntries(excludes, "excludes"),
			checkPath(val(src)))
	}
	if err == nil {
		_, err = absoluteUnixPath(*dest)
	}
	if err != nil {
		return CopyDirective{}, err
	}
	return CopyDirective{Src: *src, Dest: *dest, Includes: vals(includes, true), Excludes: vals(excludes, true), Properties: props}, nil
}

var octal = regexp.MustCompile(`^[0-7][0-7][0-7]$`)

// fileProperties is FilePropertiesSpec's creator.
func fileProperties(n *yaml.Node) (*FilePropertiesSpec, error) {
	var fileMode, dirMode, user, group, timestamp *string
	ok, err := object(n, "FilePropertiesSpec",
		prop{"filePermissions", false, setStr(&fileMode)},
		prop{"directoryPermissions", false, setStr(&dirMode)},
		prop{"user", false, setStr(&user)},
		prop{"group", false, setStr(&group)},
		prop{"timestamp", false, setStr(&timestamp)},
	)
	if !ok || err != nil {
		return nil, err
	}
	err = first(
		nullOrNotEmpty(fileMode, "filePermissions"),
		nullOrNotEmpty(dirMode, "directoryPermissions"),
		nullOrNotEmpty(user, "user"),
		nullOrNotEmpty(group, "group"),
		nullOrNotEmpty(timestamp, "timestamp"),
		permissions(fileMode),
		permissions(dirMode),
	)
	if err == nil && timestamp != nil {
		_, err = instant(*timestamp, "timestamp")
	}
	if err != nil {
		return nil, err
	}
	return &FilePropertiesSpec{FilePermissions: val(fileMode), DirectoryPermissions: val(dirMode),
		User: val(user), Group: val(group), Timestamp: val(timestamp)}, nil
}

// permissions is FilePermissions.fromOctalString's check.
func permissions(v *string) error {
	if v != nil && !octal.MatchString(*v) {
		return errors.New("octalPermissions must be a 3-digit octal number (000-777)")
	}
	return nil
}

// checkPath is Paths.get's check on Unix: no NUL.
func checkPath(p string) error {
	if strings.ContainsRune(p, 0) {
		return fmt.Errorf("Nul character not allowed: %s", p) //nolint:staticcheck // Java's message
	}
	return nil
}

// absoluteUnixPath is Jib's AbsoluteUnixPath.get: an absolute path
// without its empty components, "." and ".." kept.
func absoluteUnixPath(p string) (string, error) {
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("Path does not start with forward slash (/): %s", p) //nolint:staticcheck // Jib's message
	}
	var parts []string
	for _, c := range strings.Split(p, "/") {
		if c != "" {
			parts = append(parts, c)
		}
	}
	return "/" + strings.Join(parts, "/"), nil
}
