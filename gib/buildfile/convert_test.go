package buildfile

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/jbadeau/gib"

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
	assert.Equal(t, "", props.ownership(), "Jib's default ownership is none")
	assert.Equal(t, time.Unix(1, 0).UTC(), props.timestamp)
}

func merge(t *testing.T, base resolvedProperties, over FilePropertiesSpec) resolvedProperties {
	t.Helper()
	p, err := mergeProperties(base, &over)
	require.NoError(t, err)
	return p
}

func TestMergeProperties(t *testing.T) {
	base := defaultProperties()
	assert.Equal(t, 0755, int(merge(t, base, FilePropertiesSpec{FilePermissions: "755"}).filePermissions))
	assert.Equal(t, 0700, int(merge(t, base, FilePropertiesSpec{DirectoryPermissions: "700"}).directoryPermissions))
	assert.Equal(t, "1000:2000", merge(t, base, FilePropertiesSpec{User: "1000", Group: "2000"}).ownership())
	assert.Equal(t, ":2000", merge(t, base, FilePropertiesSpec{Group: "2000"}).ownership())
	assert.Equal(t, "app", merge(t, base, FilePropertiesSpec{User: "app"}).ownership())
	assert.Equal(t, time.UnixMilli(5000).UTC(), merge(t, base, FilePropertiesSpec{Timestamp: "5000"}).timestamp)
	assert.Equal(t, time.Date(2020, 1, 1, 0, 0, 0, 123456789, time.UTC),
		merge(t, base, FilePropertiesSpec{Timestamp: "2020-01-01T01:00:00.123456789+01:00"}).timestamp,
		"a timestamp keeps its nanoseconds")

	_, err := mergeProperties(base, &FilePropertiesSpec{Timestamp: "soon"})
	require.EqualError(t, err, "timestamp must be a number of milliseconds since epoch or an ISO 8601 formatted date")
	_, err = mergeProperties(base, &FilePropertiesSpec{FilePermissions: "0644"})
	require.Error(t, err)
}

func TestMergeProperties_Cascading(t *testing.T) {
	afterLayer := merge(t, defaultProperties(), FilePropertiesSpec{FilePermissions: "755", User: "100"})
	assert.Equal(t, 0755, int(afterLayer.filePermissions))
	assert.Equal(t, "100", afterLayer.ownership())

	afterCopy := merge(t, afterLayer, FilePropertiesSpec{FilePermissions: "444"})
	assert.Equal(t, 0444, int(afterCopy.filePermissions))
	assert.Equal(t, "100", afterCopy.ownership())
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

func TestConvert_BaseImageSchemes(t *testing.T) {
	build := func(image string) (*gib.ContainerBuilder, error) {
		spec := &BuildFileSpec{APIVersion: "v1alpha1", Kind: "BuildFile"}
		if image != "" {
			spec.From = &BaseImageSpec{Image: image, Platforms: []PlatformSpec{{"arm64", "linux"}}}
		}
		return Convert(spec, "/ctx", nil)
	}
	for _, image := range []string{"", "scratch", "registry://scratch", "tar://base/image.tar", "tar:///abs/image.tar",
		"registry://alpine:3.18", "alpine:3.18", "docker://alpine:3.18"} {
		_, err := build(image)
		assert.NoError(t, err, image)
	}
	for _, image := range []string{"Bad Ref", "registry://Bad Ref", "docker://Bad Ref"} {
		_, err := build(image)
		assert.ErrorContains(t, err, "Invalid image reference", image)
	}

	b, err := build("alpine:3.18")
	require.NoError(t, err)
	assert.Len(t, b.GetPlatforms(), 1, "a reference is built for the platforms named")
	b, err = build("tar://base.tar")
	require.NoError(t, err)
	assert.Empty(t, b.GetPlatforms(), "Jib builds a tarball base for none of them")
}

// TestConvert_RegistryScratchIsScratch builds registry://scratch with no
// registry to pull from: it is the empty image, as plain scratch is.
func TestConvert_RegistryScratchIsScratch(t *testing.T) {
	spec := &BuildFileSpec{APIVersion: "v", Kind: "BuildFile", From: &BaseImageSpec{Image: "registry://scratch"}}
	b, err := Convert(spec, t.TempDir(), nil)
	require.NoError(t, err)
	_, err = b.Containerize(context.Background(), gib.ToTar(filepath.Join(t.TempDir(), "out.tar")))
	require.NoError(t, err)
}

// TestConvert_TarBaseIsRelativeToTheWorkingDirectory: Jib reads a
// relative tar:// base from the directory it runs in, not the context.
func TestConvert_TarBaseIsRelativeToTheWorkingDirectory(t *testing.T) {
	wd := t.TempDir()
	tag, err := name.NewTag("base:1")
	require.NoError(t, err)
	require.NoError(t, tarball.WriteToFile(filepath.Join(wd, "base.tar"), tag, empty.Image))
	t.Chdir(wd)

	spec := &BuildFileSpec{APIVersion: "v", Kind: "BuildFile", From: &BaseImageSpec{Image: "tar://base.tar"}}
	b, err := Convert(spec, t.TempDir(), nil)
	require.NoError(t, err)
	_, err = b.Containerize(context.Background(), gib.ToTar(filepath.Join(t.TempDir(), "out.tar")))
	require.NoError(t, err)
}

func TestConvert_RefusesAnArchiveLayer(t *testing.T) {
	spec, err := ParseBytes([]byte(header+"layers: {entries: [{name: a, archive: a.tar}]}\n"), nil)
	require.NoError(t, err)
	_, err = Convert(spec, t.TempDir(), nil)
	require.EqualError(t, err, "Only FileLayers are supported at this time.")
}

// TestConvert_ChecksWhatWasSetAfterParsing: a value set on a parsed
// spec is refused as the build file's own would be.
func TestConvert_ChecksWhatWasSetAfterParsing(t *testing.T) {
	for _, spec := range []BuildFileSpec{
		{CreationTime: "yesterday"},
		{Format: "docker"},
		{ExposedPorts: []string{"80/TCP"}},
		{Volumes: []string{"data"}},
		{WorkingDirectory: "app"},
	} {
		_, err := Convert(&spec, t.TempDir(), nil)
		assert.Error(t, err, "%+v", spec)
	}
}
