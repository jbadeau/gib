package buildfile

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvert_AllProperties(t *testing.T) {
	spec, err := Parse(testdataFile(t, "all_properties.yaml"), nil)
	require.NoError(t, err)

	contextDir := projectDir(t)
	builder, err := Convert(spec, contextDir, nil)
	require.NoError(t, err)
	require.NotNil(t, builder)
}

func TestConvert_AllDefaults(t *testing.T) {
	spec, err := Parse(testdataFile(t, "all_defaults.yaml"), nil)
	require.NoError(t, err)

	builder, err := Convert(spec, ".", nil)
	require.NoError(t, err)
	require.NotNil(t, builder)
}

func TestConvert_TemplateParams(t *testing.T) {
	params := map[string]string{
		"baseImage": "alpine:3.18",
		"version":   "1.0.0",
		"user":      "app",
	}

	spec, err := Parse(testdataFile(t, "template_params.yaml"), params)
	require.NoError(t, err)

	builder, err := Convert(spec, ".", nil)
	require.NoError(t, err)
	require.NotNil(t, builder)
}

func TestDefaultProperties(t *testing.T) {
	props := defaultProperties()
	assert.Equal(t, 0644, int(props.filePermissions))
	assert.Equal(t, 0755, int(props.directoryPermissions))
	assert.Equal(t, "0", props.user)
	assert.Equal(t, "0", props.group)
	assert.Equal(t, int64(1000), props.timestamp)
}

func TestMergeProperties_FilePermissions(t *testing.T) {
	base := defaultProperties()
	override := &FilePropertiesSpec{FilePermissions: "755"}
	result := mergeProperties(base, override)
	assert.Equal(t, 0755, int(result.filePermissions))
}

func TestMergeProperties_DirectoryPermissions(t *testing.T) {
	base := defaultProperties()
	override := &FilePropertiesSpec{DirectoryPermissions: "700"}
	result := mergeProperties(base, override)
	assert.Equal(t, 0700, int(result.directoryPermissions))
}

func TestMergeProperties_UserGroup(t *testing.T) {
	base := defaultProperties()
	override := &FilePropertiesSpec{User: "1000", Group: "2000"}
	result := mergeProperties(base, override)
	assert.Equal(t, "1000", result.user)
	assert.Equal(t, "2000", result.group)
}

func TestMergeProperties_Timestamp(t *testing.T) {
	base := defaultProperties()
	override := &FilePropertiesSpec{Timestamp: "5000"}
	result := mergeProperties(base, override)
	assert.Equal(t, int64(5000), result.timestamp)
}

func TestMergeProperties_Cascading(t *testing.T) {
	global := defaultProperties()
	layerOverride := &FilePropertiesSpec{FilePermissions: "755", User: "100"}
	copyOverride := &FilePropertiesSpec{FilePermissions: "444"}

	// Global -> Layer
	afterLayer := mergeProperties(global, layerOverride)
	assert.Equal(t, 0755, int(afterLayer.filePermissions))
	assert.Equal(t, "100", afterLayer.user)
	assert.Equal(t, "0", afterLayer.group) // unchanged

	// Layer -> Copy
	afterCopy := mergeProperties(afterLayer, copyOverride)
	assert.Equal(t, 0444, int(afterCopy.filePermissions)) // overridden
	assert.Equal(t, "100", afterCopy.user)                // inherited from layer
}

func TestParseTimestamp_Millis(t *testing.T) {
	ms, err := parseTimestamp("1000")
	require.NoError(t, err)
	assert.Equal(t, int64(1000), ms)
}

func TestParseTimestamp_ISO8601(t *testing.T) {
	ms, err := parseTimestamp("2020-01-01T00:00:00Z")
	require.NoError(t, err)
	assert.Equal(t, int64(1577836800000), ms)
}

func TestParseTimestamp_Invalid(t *testing.T) {
	_, err := parseTimestamp("not-a-time")
	require.Error(t, err)
}

func TestConvert_WithSourceCredentials(t *testing.T) {
	spec := &BuildFileSpec{
		APIVersion: "jib/v1alpha1",
		Kind:       "BuildFile",
		From:       &BaseImageSpec{Image: "ubuntu:22.04"},
	}

	opts := &ConvertOptions{
		FromUsername: "user",
		FromPassword: "pass",
	}

	builder, err := Convert(spec, ".", opts)
	require.NoError(t, err)
	require.NotNil(t, builder)
}

func TestConvert_WithSourceCredentialHelper(t *testing.T) {
	spec := &BuildFileSpec{
		APIVersion: "jib/v1alpha1",
		Kind:       "BuildFile",
		From:       &BaseImageSpec{Image: "gcr.io/proj/app:latest"},
	}

	opts := &ConvertOptions{
		FromCredentialHelper: "gcr",
	}

	builder, err := Convert(spec, ".", opts)
	require.NoError(t, err)
	require.NotNil(t, builder)
}

func TestConvert_WithInsecureRegistry(t *testing.T) {
	spec := &BuildFileSpec{
		APIVersion: "jib/v1alpha1",
		Kind:       "BuildFile",
		From:       &BaseImageSpec{Image: "localhost:5000/image:latest"},
	}

	opts := &ConvertOptions{
		AllowInsecureRegistries: true,
	}

	builder, err := Convert(spec, ".", opts)
	require.NoError(t, err)
	require.NotNil(t, builder)
}

func TestConvert_NilOptions(t *testing.T) {
	spec := &BuildFileSpec{
		APIVersion: "jib/v1alpha1",
		Kind:       "BuildFile",
	}

	builder, err := Convert(spec, ".", nil)
	require.NoError(t, err)
	require.NotNil(t, builder)
}

func TestConvert_AlternativeRootContext(t *testing.T) {
	// Use the testdata/projects/simple directory as an alternative root context
	altContextDir := projectDir(t)

	spec, err := Parse(testdataFile(t, "all_properties.yaml"), nil)
	require.NoError(t, err)

	builder, err := Convert(spec, altContextDir, nil)
	require.NoError(t, err)
	require.NotNil(t, builder)
}

func TestConvert_Platforms(t *testing.T) {
	spec := &BuildFileSpec{
		APIVersion: "jib/v1alpha1",
		Kind:       "BuildFile",
		From: &BaseImageSpec{
			Image: "ubuntu:22.04",
			Platforms: []PlatformSpec{
				{Architecture: "arm", OS: "linux"},
				{Architecture: "amd64", OS: "linux"},
			},
		},
	}

	builder, err := Convert(spec, ".", nil)
	require.NoError(t, err)
	require.NotNil(t, builder)
	assert.Len(t, builder.GetPlatforms(), 2)
}

func projectDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "testdata", "projects", "simple"))
	require.NoError(t, err)
	return dir
}
