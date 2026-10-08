package buildfile

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_AllDefaults(t *testing.T) {
	spec, err := Parse(testdataFile(t, "all_defaults.yaml"), nil)
	require.NoError(t, err)

	assert.Equal(t, "jib/v1alpha1", spec.APIVersion)
	assert.Equal(t, "BuildFile", spec.Kind)
	assert.Nil(t, spec.From)
	assert.Empty(t, spec.CreationTime)
	assert.Empty(t, spec.Format)
	assert.Nil(t, spec.Environment)
	assert.Nil(t, spec.Labels)
	assert.Nil(t, spec.Volumes)
	assert.Nil(t, spec.ExposedPorts)
	assert.Empty(t, spec.User)
	assert.Empty(t, spec.WorkingDirectory)
	assert.Nil(t, spec.Entrypoint)
	assert.Nil(t, spec.Cmd)
	assert.Nil(t, spec.Layers)
}

func TestParse_AllProperties(t *testing.T) {
	spec, err := Parse(testdataFile(t, "all_properties.yaml"), nil)
	require.NoError(t, err)

	require.NotNil(t, spec.From)
	assert.Equal(t, "ubuntu:22.04", spec.From.Image)
	assert.Equal(t, []PlatformSpec{{"amd64", "linux"}, {"arm64", "linux"}}, spec.From.Platforms)
	assert.Equal(t, "2000", spec.CreationTime)
	assert.Equal(t, "Docker", spec.Format)
	assert.Equal(t, map[string]string{"KEY1": "val1", "KEY2": "val2"}, spec.Environment)
	assert.Equal(t, map[string]string{"label1": "value1", "label2": "value2"}, spec.Labels)
	assert.Equal(t, []string{"/vol1", "/vol2"}, spec.Volumes)
	assert.Equal(t, []string{"8080", "123/udp"}, spec.ExposedPorts)
	assert.Equal(t, "customUser", spec.User)
	assert.Equal(t, "/home", spec.WorkingDirectory)
	assert.Equal(t, []string{"sh", "script.sh"}, spec.Entrypoint)
	assert.Equal(t, []string{"--param"}, spec.Cmd)
	require.NotNil(t, spec.Layers)
	assert.Equal(t, &FilePropertiesSpec{"644", "755", "0", "0", "1000"}, spec.Layers.Properties)
	require.Len(t, spec.Layers.Entries, 2)
	assert.Equal(t, "scripts", spec.Layers.Entries[0].Name)
	assert.Equal(t, "444", spec.Layers.Entries[1].Properties.FilePermissions)
}

func TestParse_TemplateParameters(t *testing.T) {
	spec, err := Parse(testdataFile(t, "template_params.yaml"),
		map[string]string{"baseImage": "alpine:3.18", "version": "1.0.0", "user": "app"})
	require.NoError(t, err)
	assert.Equal(t, "alpine:3.18", spec.From.Image)
	assert.Equal(t, "1.0.0", spec.Environment["VERSION"])
	assert.Equal(t, "app", spec.User)

	_, err = Parse(testdataFile(t, "template_params.yaml"), map[string]string{"baseImage": "alpine:3.18"})
	require.EqualError(t, err, "Cannot resolve variable 'version' (enableSubstitutionInVariables=false).")
}

const header = "apiVersion: jib/v1alpha1\nkind: BuildFile\n"

// TestParseReadsWhatJibReads holds build files Jib accepts.
func TestParseReadsWhatJibReads(t *testing.T) {
	cases := []struct {
		name, file string
		check      func(t *testing.T, s *BuildFileSpec)
	}{
		{"any apiVersion that is not empty", "apiVersion: whatever\nkind: BuildFile\n",
			func(t *testing.T, s *BuildFileSpec) { assert.Equal(t, "whatever", s.APIVersion) }},
		{"a parameter's default", header + "user: ${who:-nobody}\n",
			func(t *testing.T, s *BuildFileSpec) { assert.Equal(t, "nobody", s.User) }},
		{"an escaped parameter", header + "user: $${who}\n",
			func(t *testing.T, s *BuildFileSpec) { assert.Equal(t, "${who}", s.User) }},
		{"an unclosed parameter as it is", header + "user: ${who\n",
			func(t *testing.T, s *BuildFileSpec) { assert.Equal(t, "${who", s.User) }},
		{"a scalar as written", header + "user: 0644\ncmd: [0x1F, yes, 1_000, .inf]\n",
			func(t *testing.T, s *BuildFileSpec) {
				assert.Equal(t, "0644", s.User)
				assert.Equal(t, []string{"0x1F", "yes", "1_000", ".inf"}, s.Cmd)
			}},
		{"an alias as what it names", header + "entrypoint: &e [a, b]\ncmd: *e\n",
			func(t *testing.T, s *BuildFileSpec) { assert.Equal(t, []string{"a", "b"}, s.Cmd) }},
		{"null as unset", header + "user: ~\ncmd: null\nlayers:\n",
			func(t *testing.T, s *BuildFileSpec) {
				assert.Empty(t, s.User)
				assert.Nil(t, s.Cmd)
				assert.Nil(t, s.Layers)
			}},
		{"an empty list as set to nothing", header + "entrypoint: []\n",
			func(t *testing.T, s *BuildFileSpec) { assert.Equal(t, []string{}, s.Entrypoint) }},
		{"a port range", header + "exposedPorts: [8000-8002/udp]\n",
			func(t *testing.T, s *BuildFileSpec) { assert.Equal(t, []string{"8000-8002/udp"}, s.ExposedPorts) }},
		{"an archive layer, which it builds no further", header + "layers: {entries: [{name: a, archive: a.tar}]}\n",
			func(t *testing.T, s *BuildFileSpec) { assert.Equal(t, "a.tar", s.Layers.Entries[0].Archive) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := ParseBytes([]byte(c.file), nil)
			require.NoError(t, err)
			c.check(t, s)
		})
	}
}

// TestParseRefusesWhatJibRefuses holds build files Jib refuses, and why.
func TestParseRefusesWhatJibRefuses(t *testing.T) {
	cases := []struct{ name, file, err string }{
		{"no apiVersion", "kind: BuildFile\n", "Missing required creator property 'apiVersion'"},
		{"an empty apiVersion", "apiVersion: ' '\nkind: BuildFile\n", "Property 'apiVersion' cannot be an empty string"},
		{"no kind", "apiVersion: v\n", "Missing required creator property 'kind'"},
		{"another kind", "apiVersion: v\nkind: Other\n", "Property 'kind' must be 'BuildFile' but is 'Other'"},
		{"an undefined parameter", header + "user: ${who}\n", "Cannot resolve variable 'who'"},
		{"an unknown property", header + "entryPoint: [a]\n", `Unrecognized field "entryPoint"`},
		{"an unknown property of a layer", header + "layers: {entries: [{name: a, files: [], mode: x}]}\n", `Unrecognized field "mode"`},
		{"a key twice", header + "user: a\nuser: b\n", `mapping key "user" already defined`},
		{"a list for a string", header + "user: [a]\n", "expected a string"},
		{"a string for a list", header + "cmd: run\n", "expected a list"},
		{"an empty creationTime", header + "creationTime: ''\n", "Property 'creationTime' cannot be an empty string"},
		{"an empty format", header + "format: ''\n", "Property 'format' cannot be an empty string"},
		{"an empty user", header + "user: ' '\n", "Property 'user' cannot be an empty string"},
		{"an empty workingDirectory", header + "workingDirectory: ''\n", "Property 'workingDirectory' cannot be an empty string"},
		{"an empty environment value", header + "environment: {A: ''}\n", "Property 'environment' cannot contain empty string values"},
		{"a null environment value", header + "environment: {A: ~}\n", "Property 'environment' cannot contain null values"},
		{"an empty label key", header + "labels: {'': x}\n", "Property 'labels' cannot contain empty string keys"},
		{"an empty volume", header + "volumes: ['']\n", "Property 'volumes' cannot contain empty strings"},
		{"a null command entry", header + "cmd: [a, ~]\n", "Property 'cmd' cannot contain null entries"},
		{"an empty entrypoint entry", header + "entrypoint: ['']\n", "Property 'entrypoint' cannot contain empty strings"},
		{"a format other than Docker or OCI", header + "format: docker\n", "No enum constant com.google.cloud.tools.jib.api.buildplan.ImageFormat.docker"},
		{"a date with no time", header + "creationTime: 2020-01-01\n", "creationTime must be a number of milliseconds since epoch or an ISO 8601 formatted date"},
		{"a time with no zone", header + "creationTime: 2020-01-01T00:00:00\n", "creationTime must be"},
		{"a relative volume", header + "volumes: [data]\n", "Path does not start with forward slash (/): data"},
		{"a relative workingDirectory", header + "workingDirectory: app\n", "Path does not start with forward slash (/): app"},
		{"an upper case protocol", header + "exposedPorts: [80/TCP]\n", "Invalid port configuration: '80/TCP'"},
		{"a signed port", header + "exposedPorts: [+80]\n", "Invalid port configuration: '+80'"},
		{"a port out of range", header + "exposedPorts: [0]\n", "Port number '0' is out of usual range (1-65535)."},
		{"a range backwards", header + "exposedPorts: [9-8]\n", "Invalid port range '9-8'; smaller number must come first."},
		{"a base image of no image", header + "from: {}\n", "Missing required creator property 'image'"},
		{"a base image of an empty image", header + "from: {image: ''}\n", "Property 'image' cannot be an empty string"},
		{"a platform with no os", header + "from: {image: a, platforms: [{architecture: arm64}]}\n", "Missing required creator property 'os'"},
		{"a null platform", header + "from: {image: a, platforms: [~]}\n", "Property 'platforms' cannot contain null entries"},
		{"layers with no entries", header + "layers: {}\n", "Missing required creator property 'entries'"},
		{"layers with empty entries", header + "layers: {entries: []}\n", "Property 'entries' cannot be an empty collection"},
		{"a null layer", header + "layers: {entries: [~]}\n", "a layer cannot be null"},
		{"a layer with no name", header + "layers: {entries: [{files: [{src: a, dest: /a}]}]}\n", "Could not parse layer entry, missing required property 'name'"},
		{"a layer with neither files nor archive", header + "layers: {entries: [{name: a}]}\n", "Could not parse entry into ArchiveLayer or FileLayer"},
		{"a layer of no files", header + "layers: {entries: [{name: a, files: []}]}\n", "Property 'files' cannot be an empty collection"},
		{"a layer of null files", header + "layers: {entries: [{name: a, files: ~}]}\n", "Property 'files' cannot be null"},
		{"a null copy", header + "layers: {entries: [{name: a, files: [~]}]}\n", "a copy cannot be null"},
		{"a copy of an empty src", header + "layers: {entries: [{name: a, files: [{src: '', dest: /a}]}]}\n", "Property 'src' cannot be an empty string"},
		{"a copy with no dest", header + "layers: {entries: [{name: a, files: [{src: a}]}]}\n", "Missing required creator property 'dest'"},
		{"a copy to a relative dest", header + "layers: {entries: [{name: a, files: [{src: a, dest: app}]}]}\n", "Path does not start with forward slash (/): app"},
		{"an empty include", header + "layers: {entries: [{name: a, files: [{src: a, dest: /a, includes: ['']}]}]}\n", "Property 'includes' cannot contain empty strings"},
		{"permissions not three octal digits", header + "layers: {properties: {filePermissions: '0644'}, entries: [{name: a, files: [{src: a, dest: /a}]}]}\n", "octalPermissions must be a 3-digit octal number (000-777)"},
		{"an empty group", header + "layers: {properties: {group: ''}, entries: [{name: a, files: [{src: a, dest: /a}]}]}\n", "Property 'group' cannot be an empty string"},
		{"a timestamp that is no time", header + "layers: {properties: {timestamp: soon}, entries: [{name: a, files: [{src: a, dest: /a}]}]}\n", "timestamp must be a number of milliseconds since epoch or an ISO 8601 formatted date"},
		{"an archive layer with files", header + "layers: {entries: [{name: a, archive: a.tar, files: []}]}\n", `Unrecognized field "files"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseBytes([]byte(c.file), nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.err)
		})
	}
}

func testdataFile(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "testdata", "buildfiles", name))
	require.NoError(t, err)
	return path
}
