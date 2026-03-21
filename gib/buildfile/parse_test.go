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

	assert.Equal(t, "jib/v1alpha1", spec.APIVersion)
	assert.Equal(t, "BuildFile", spec.Kind)

	require.NotNil(t, spec.From)
	assert.Equal(t, "ubuntu:22.04", spec.From.Image)
	assert.Len(t, spec.From.Platforms, 2)
	assert.Equal(t, "amd64", spec.From.Platforms[0].Architecture)
	assert.Equal(t, "linux", spec.From.Platforms[0].OS)

	assert.Equal(t, "2000", spec.CreationTime)
	assert.Equal(t, "Docker", spec.Format)

	assert.Equal(t, "val1", spec.Environment["KEY1"])
	assert.Equal(t, "val2", spec.Environment["KEY2"])

	assert.Equal(t, "value1", spec.Labels["label1"])
	assert.Equal(t, "value2", spec.Labels["label2"])

	assert.Equal(t, []string{"/vol1", "/vol2"}, spec.Volumes)
	assert.Equal(t, []string{"8080", "123/udp"}, spec.ExposedPorts)

	assert.Equal(t, "customUser", spec.User)
	assert.Equal(t, "/home", spec.WorkingDirectory)
	assert.Equal(t, []string{"sh", "script.sh"}, spec.Entrypoint)
	assert.Equal(t, []string{"--param"}, spec.Cmd)

	require.NotNil(t, spec.Layers)
	require.NotNil(t, spec.Layers.Properties)
	assert.Equal(t, "644", spec.Layers.Properties.FilePermissions)
	assert.Equal(t, "755", spec.Layers.Properties.DirectoryPermissions)
	assert.Equal(t, "0", spec.Layers.Properties.User)
	assert.Equal(t, "0", spec.Layers.Properties.Group)
	assert.Equal(t, "1000", spec.Layers.Properties.Timestamp)

	assert.Len(t, spec.Layers.Entries, 2)
	assert.Equal(t, "scripts", spec.Layers.Entries[0].Name)
	assert.Equal(t, "images", spec.Layers.Entries[1].Name)
	assert.Equal(t, "444", spec.Layers.Entries[1].Properties.FilePermissions)
}

func TestParse_MissingAPIVersion(t *testing.T) {
	_, err := Parse(testdataFile(t, "invalid_missing_api.yaml"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiVersion")
}

func TestParse_MissingKind(t *testing.T) {
	_, err := Parse(testdataFile(t, "invalid_missing_kind.yaml"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kind")
}

func TestParse_TemplateSubstitution(t *testing.T) {
	params := map[string]string{
		"baseImage": "alpine:3.18",
		"version":   "1.0.0",
		"user":      "app",
	}

	spec, err := Parse(testdataFile(t, "template_params.yaml"), params)
	require.NoError(t, err)

	assert.Equal(t, "alpine:3.18", spec.From.Image)
	assert.Equal(t, "1.0.0", spec.Environment["VERSION"])
	assert.Equal(t, "app", spec.User)
}

func TestParse_TemplateMissingParam(t *testing.T) {
	params := map[string]string{
		"baseImage": "alpine:3.18",
		// Missing "version" and "user"
	}

	_, err := Parse(testdataFile(t, "template_params.yaml"), params)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required template parameter")
}

func TestParseBytes_InvalidYAML(t *testing.T) {
	_, err := ParseBytes([]byte("not: valid: yaml: ["), nil)
	require.Error(t, err)
}

func TestParseBytes_WrongAPIVersion(t *testing.T) {
	yaml := `apiVersion: wrong/v1
kind: BuildFile`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiVersion")
}

func TestParseBytes_WrongKind(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: WrongKind`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kind")
}

func TestSubstituteParams_NoParams(t *testing.T) {
	result, err := substituteParams("hello world", nil)
	require.NoError(t, err)
	assert.Equal(t, "hello world", result)
}

func TestSubstituteParams_SingleParam(t *testing.T) {
	result, err := substituteParams("image: $${name}", map[string]string{"name": "alpine"})
	require.NoError(t, err)
	assert.Equal(t, "image: alpine", result)
}

func TestSubstituteParams_MultipleParams(t *testing.T) {
	result, err := substituteParams("$${a}:$${b}", map[string]string{"a": "x", "b": "y"})
	require.NoError(t, err)
	assert.Equal(t, "x:y", result)
}

func TestSubstituteParams_UnclosedBrace(t *testing.T) {
	_, err := substituteParams("$${unclosed", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unclosed")
}

// --- Validation tests ported from Jib's BuildFileSpecTest ---

func TestParseBytes_EmptyAPIVersion(t *testing.T) {
	yaml := `apiVersion: "   "
kind: BuildFile`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiVersion")
	assert.Contains(t, err.Error(), "empty")
}

func TestParseBytes_EmptyKind(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: "   "`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kind")
	assert.Contains(t, err.Error(), "empty")
}

func TestParse_EmptyStringInVolumes(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile
volumes:
  - "/valid"
  - "   "`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "volumes")
	assert.Contains(t, err.Error(), "empty")
}

func TestParse_EmptyStringInExposedPorts(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile
exposedPorts:
  - "   "`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exposedPorts")
	assert.Contains(t, err.Error(), "empty")
}

func TestParse_EmptyStringInEntrypoint(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile
entrypoint:
  - "   "`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entrypoint")
	assert.Contains(t, err.Error(), "empty")
}

func TestParse_EmptyStringInCmd(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile
cmd:
  - "   "`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cmd")
	assert.Contains(t, err.Error(), "empty")
}

func TestParse_EmptyEnvironmentValue(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile
environment:
  KEY: "   "`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "environment")
	assert.Contains(t, err.Error(), "empty")
}

func TestParse_EmptyLabelValue(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile
labels:
  key: "   "`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "labels")
	assert.Contains(t, err.Error(), "empty")
}

func TestParse_EmptyListOkay(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile
volumes: []
exposedPorts: []
entrypoint: []
cmd: []`
	spec, err := ParseBytes([]byte(yaml), nil)
	require.NoError(t, err)
	assert.Empty(t, spec.Volumes)
	assert.Empty(t, spec.ExposedPorts)
	assert.Empty(t, spec.Entrypoint)
	assert.Empty(t, spec.Cmd)
}

func TestParse_EmptyMapOkay(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile
environment: {}
labels: {}`
	spec, err := ParseBytes([]byte(yaml), nil)
	require.NoError(t, err)
	assert.Empty(t, spec.Environment)
	assert.Empty(t, spec.Labels)
}

func TestParse_EmptyCreationTime(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile
creationTime: "   "`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creationTime")
	assert.Contains(t, err.Error(), "empty")
}

func TestParse_EmptyFormat(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile
format: "   "`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "format")
	assert.Contains(t, err.Error(), "empty")
}

func TestParse_EmptyUser(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile
user: "   "`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user")
	assert.Contains(t, err.Error(), "empty")
}

func TestParse_EmptyWorkingDirectory(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile
workingDirectory: "   "`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workingDirectory")
	assert.Contains(t, err.Error(), "empty")
}

func TestParse_InvalidFilePermissions(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile
layers:
  properties:
    filePermissions: "888"
  entries:
    - name: test
      files:
        - src: .
          dest: /app`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "filePermissions")
	assert.Contains(t, err.Error(), "octal")
}

func TestParse_InvalidDirectoryPermissions(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile
layers:
  properties:
    directoryPermissions: "999"
  entries:
    - name: test
      files:
        - src: .
          dest: /app`
	_, err := ParseBytes([]byte(yaml), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "directoryPermissions")
	assert.Contains(t, err.Error(), "octal")
}

func TestParse_ValidPermissions(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile
layers:
  properties:
    filePermissions: "644"
    directoryPermissions: "755"
  entries:
    - name: test
      files:
        - src: .
          dest: /app`
	spec, err := ParseBytes([]byte(yaml), nil)
	require.NoError(t, err)
	assert.Equal(t, "644", spec.Layers.Properties.FilePermissions)
	assert.Equal(t, "755", spec.Layers.Properties.DirectoryPermissions)
}

func TestParse_NullCollections(t *testing.T) {
	yaml := `apiVersion: jib/v1alpha1
kind: BuildFile`
	spec, err := ParseBytes([]byte(yaml), nil)
	require.NoError(t, err)
	assert.Nil(t, spec.Environment)
	assert.Nil(t, spec.Labels)
	assert.Nil(t, spec.Volumes)
	assert.Nil(t, spec.ExposedPorts)
	assert.Nil(t, spec.Entrypoint)
	assert.Nil(t, spec.Cmd)
}

func testdataFile(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "testdata", "buildfiles", name))
	require.NoError(t, err)
	return path
}
