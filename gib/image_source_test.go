package gib

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistrySource_DefaultKeychain(t *testing.T) {
	src := RegistrySource("gcr.io/project/image:latest")
	rs, ok := src.(*registrySource)
	require.True(t, ok)
	assert.False(t, rs.hasAuth)
	assert.Empty(t, rs.authOptions)
	assert.Empty(t, rs.nameOptions)
}

func TestRegistrySource_WithSourceCredentials(t *testing.T) {
	src := RegistrySource("gcr.io/project/image:latest",
		WithSourceCredentials("user", "pass"),
	)
	rs, ok := src.(*registrySource)
	require.True(t, ok)
	assert.True(t, rs.hasAuth)
	assert.Len(t, rs.authOptions, 1)
}

func TestRegistrySource_WithSourceCredentialHelper(t *testing.T) {
	src := RegistrySource("gcr.io/project/image:latest",
		WithSourceCredentialHelper("gcr"),
	)
	rs, ok := src.(*registrySource)
	require.True(t, ok)
	assert.True(t, rs.hasAuth)
	assert.Len(t, rs.authOptions, 1)
}

func TestRegistrySource_WithSourceInsecure(t *testing.T) {
	src := RegistrySource("localhost:5000/image:latest",
		WithSourceInsecure(),
	)
	rs, ok := src.(*registrySource)
	require.True(t, ok)
	assert.Len(t, rs.nameOptions, 1)
}

func TestRegistrySource_MultipleOptions(t *testing.T) {
	src := RegistrySource("localhost:5000/image:latest",
		WithSourceCredentials("user", "pass"),
		WithSourceInsecure(),
	)
	rs, ok := src.(*registrySource)
	require.True(t, ok)
	assert.True(t, rs.hasAuth)
	assert.Len(t, rs.authOptions, 1)
	assert.Len(t, rs.nameOptions, 1)
}

func TestTarSource_Constructor(t *testing.T) {
	src := TarSource("/path/to/image.tar")
	_, ok := src.(*tarSource)
	assert.True(t, ok)
}

func TestFrom_WithSourceOptions(t *testing.T) {
	builder := From("gcr.io/project/image:latest",
		WithSourceCredentials("user", "pass"),
		WithSourceInsecure(),
	)
	require.NotNil(t, builder)

	rs, ok := builder.source.(*registrySource)
	require.True(t, ok)
	assert.True(t, rs.hasAuth)
	assert.Len(t, rs.nameOptions, 1)
}

func TestFrom_NoOptions(t *testing.T) {
	builder := From("gcr.io/project/image:latest")
	require.NotNil(t, builder)

	rs, ok := builder.source.(*registrySource)
	require.True(t, ok)
	assert.False(t, rs.hasAuth)
}
