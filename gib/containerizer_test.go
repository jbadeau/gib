package gib

import (
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/stretchr/testify/assert"
)

func TestToRegistry(t *testing.T) {
	c := ToRegistry("gcr.io/proj/app:v1")
	assert.Equal(t, "registry", c.targetType)
	assert.Equal(t, "gcr.io/proj/app:v1", c.registryRef)
}

func TestToTar(t *testing.T) {
	c := ToTar("/output/image.tar")
	assert.Equal(t, "tar", c.targetType)
	assert.Equal(t, "/output/image.tar", c.tarPath)
}

func TestWithAdditionalTag(t *testing.T) {
	c := ToRegistry("gcr.io/proj/app:v1", WithAdditionalTag("latest"))
	assert.Equal(t, []string{"latest"}, c.additionalTags)
}

func TestWithCredentialHelper(t *testing.T) {
	c := ToRegistry("gcr.io/proj/app:v1", WithCredentialHelper("gcr"))
	assert.Equal(t, "gcr", c.credentialHelper)
}

func TestWithCredentials(t *testing.T) {
	c := ToRegistry("gcr.io/proj/app:v1", WithCredentials("user", "pass"))
	assert.Equal(t, "user", c.username)
	assert.Equal(t, "pass", c.password)
}

func TestWithAllowInsecureRegistries(t *testing.T) {
	c := ToRegistry("localhost:5000/app:v1", WithAllowInsecureRegistries(true))
	assert.True(t, c.allowInsecureRegistries)
}

func TestWithSendCredentialsOverHTTP(t *testing.T) {
	c := ToRegistry("localhost:5000/app:v1", WithSendCredentialsOverHTTP(true))
	assert.True(t, c.sendCredentialsOverHTTP)
}

func TestWithTarImageName(t *testing.T) {
	c := ToTar("/output/image.tar", WithTarImageName("myapp:v1"))
	assert.Equal(t, "myapp:v1", c.tarImageName)
}

func TestMultipleOptions(t *testing.T) {
	c := ToRegistry("gcr.io/proj/app:v1",
		WithAdditionalTag("latest"),
		WithAdditionalTag("v1.0"),
		WithCredentialHelper("gcr"),
		WithAllowInsecureRegistries(true),
	)
	assert.Equal(t, []string{"latest", "v1.0"}, c.additionalTags)
	assert.Equal(t, "gcr", c.credentialHelper)
	assert.True(t, c.allowInsecureRegistries)
}

func TestContainerizer_InsecureRegistryNameOptions(t *testing.T) {
	c := ToRegistry("localhost:5000/app:v1", WithAllowInsecureRegistries(true))
	nameOpts := c.nameOptions()
	assert.Len(t, nameOpts, 1)

	// Verify the name option enables HTTP scheme
	ref, err := name.ParseReference("localhost:5000/app:v1", nameOpts...)
	assert.NoError(t, err)
	assert.Equal(t, "http", ref.Context().Scheme())
}

func TestContainerizer_SecureRegistryNameOptions(t *testing.T) {
	c := ToRegistry("gcr.io/proj/app:v1")
	nameOpts := c.nameOptions()
	assert.Empty(t, nameOpts)
}

func TestContainerizer_CredentialHelperStored(t *testing.T) {
	c := ToRegistry("gcr.io/proj/app:v1", WithCredentialHelper("gcr"))
	assert.Equal(t, "gcr", c.credentialHelper)
	// Credential helper is now wired in writeRegistry() — it creates a keychain
}

func TestContainerizer_AuthPriority_ExplicitOverHelper(t *testing.T) {
	c := ToRegistry("gcr.io/proj/app:v1",
		WithCredentials("user", "pass"),
		WithCredentialHelper("gcr"),
	)
	// Explicit credentials should be set
	assert.Equal(t, "user", c.username)
	assert.Equal(t, "pass", c.password)
	// Helper also set — but writeRegistry uses explicit creds first
	assert.Equal(t, "gcr", c.credentialHelper)
}

func TestContainerizer_DefaultConfiguration(t *testing.T) {
	c := ToRegistry("gcr.io/proj/app:v1")
	assert.False(t, c.allowInsecureRegistries)
	assert.False(t, c.sendCredentialsOverHTTP)
	assert.Empty(t, c.additionalTags)
	assert.Empty(t, c.credentialHelper)
	assert.Empty(t, c.username)
	assert.Empty(t, c.password)
}

func TestContainerizer_WithValues(t *testing.T) {
	c := ToRegistry("gcr.io/proj/app:v1",
		WithCredentialHelper("gcr"),
		WithAllowInsecureRegistries(true),
		WithSendCredentialsOverHTTP(true),
		WithAdditionalTag("tag1"),
		WithAdditionalTag("tag2"),
	)
	assert.True(t, c.allowInsecureRegistries)
	assert.True(t, c.sendCredentialsOverHTTP)
	assert.Equal(t, []string{"tag1", "tag2"}, c.additionalTags)
	assert.Equal(t, "gcr", c.credentialHelper)
}

func TestContainerizer_TarImage(t *testing.T) {
	c := ToTar("/output/image.tar", WithTarImageName("myapp:v1"))
	assert.Equal(t, "tar", c.targetType)
	assert.Equal(t, "/output/image.tar", c.tarPath)
	assert.Equal(t, "myapp:v1", c.tarImageName)
}

func TestContainerizer_RegistryImage(t *testing.T) {
	c := ToRegistry("gcr.io/proj/app:v1",
		WithCredentials("user", "pass"),
		WithAdditionalTag("latest"),
	)
	assert.Equal(t, "registry", c.targetType)
	assert.Equal(t, "gcr.io/proj/app:v1", c.registryRef)
	assert.Equal(t, "user", c.username)
	assert.Equal(t, []string{"latest"}, c.additionalTags)
}
