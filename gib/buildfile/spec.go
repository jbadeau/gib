package buildfile

// BuildFileSpec represents a jib.yaml build file. An empty string is a
// property the build file does not set; Parse refuses an empty one it
// does set, as Jib does.
type BuildFileSpec struct {
	APIVersion       string
	Kind             string
	From             *BaseImageSpec
	CreationTime     string
	Format           string
	Environment      map[string]string
	Labels           map[string]string
	Volumes          []string
	ExposedPorts     []string
	User             string
	WorkingDirectory string
	Entrypoint       []string // nil is unset; empty is set to nothing
	Cmd              []string // nil is unset; empty is set to nothing
	Layers           *LayersSpec
}

// BaseImageSpec specifies the base image.
type BaseImageSpec struct {
	Image     string
	Platforms []PlatformSpec
}

// PlatformSpec specifies a target platform.
type PlatformSpec struct {
	Architecture string
	OS           string
}

// LayersSpec specifies layers to add.
type LayersSpec struct {
	Properties *FilePropertiesSpec
	Entries    []LayerEntrySpec
}

// LayerEntrySpec specifies a single layer: a file layer, which has
// files, or an archive layer, which Jib reads but does not build.
type LayerEntrySpec struct {
	Name       string
	Properties *FilePropertiesSpec
	Files      []CopyDirective
	Archive    string
	MediaType  string
}

// CopyDirective specifies files to copy into a layer.
type CopyDirective struct {
	Src        string
	Dest       string
	Excludes   []string
	Includes   []string
	Properties *FilePropertiesSpec
}

// FilePropertiesSpec specifies file properties.
type FilePropertiesSpec struct {
	FilePermissions      string
	DirectoryPermissions string
	User                 string
	Group                string
	Timestamp            string
}
