package buildfile

// BuildFileSpec represents a jib.yaml build file.
type BuildFileSpec struct {
	APIVersion       string            `yaml:"apiVersion"`
	Kind             string            `yaml:"kind"`
	From             *BaseImageSpec    `yaml:"from,omitempty"`
	CreationTime     string            `yaml:"creationTime,omitempty"`
	Format           string            `yaml:"format,omitempty"`
	Environment      map[string]string `yaml:"environment,omitempty"`
	Labels           map[string]string `yaml:"labels,omitempty"`
	Volumes          []string          `yaml:"volumes,omitempty"`
	ExposedPorts     []string          `yaml:"exposedPorts,omitempty"`
	User             string            `yaml:"user,omitempty"`
	WorkingDirectory string            `yaml:"workingDirectory,omitempty"`
	Entrypoint       []string          `yaml:"entrypoint,omitempty"`
	Cmd              []string          `yaml:"cmd,omitempty"`
	Layers           *LayersSpec       `yaml:"layers,omitempty"`
}

// BaseImageSpec specifies the base image.
type BaseImageSpec struct {
	Image     string         `yaml:"image,omitempty"`
	Platforms []PlatformSpec `yaml:"platforms,omitempty"`
}

// PlatformSpec specifies a target platform.
type PlatformSpec struct {
	Architecture string `yaml:"architecture"`
	OS           string `yaml:"os"`
}

// LayersSpec specifies layers to add.
type LayersSpec struct {
	Properties *FilePropertiesSpec `yaml:"properties,omitempty"`
	Entries    []LayerEntrySpec    `yaml:"entries,omitempty"`
}

// LayerEntrySpec specifies a single layer.
type LayerEntrySpec struct {
	Name       string              `yaml:"name,omitempty"`
	Properties *FilePropertiesSpec `yaml:"properties,omitempty"`
	Files      []CopyDirective     `yaml:"files,omitempty"`
}

// CopyDirective specifies files to copy into a layer.
type CopyDirective struct {
	Src        string              `yaml:"src"`
	Dest       string              `yaml:"dest"`
	Excludes   []string            `yaml:"excludes,omitempty"`
	Includes   []string            `yaml:"includes,omitempty"`
	Properties *FilePropertiesSpec `yaml:"properties,omitempty"`
}

// FilePropertiesSpec specifies file properties.
type FilePropertiesSpec struct {
	FilePermissions      string `yaml:"filePermissions,omitempty"`
	DirectoryPermissions string `yaml:"directoryPermissions,omitempty"`
	User                 string `yaml:"user,omitempty"`
	Group                string `yaml:"group,omitempty"`
	Timestamp            string `yaml:"timestamp,omitempty"`
}
