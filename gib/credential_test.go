package gib

import (
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCredentialHelper_Get_Success(t *testing.T) {
	h := &credentialHelper{
		suffix: "test",
		execFn: func(serverURL string) (string, string, error) {
			assert.Equal(t, "registry.example.com", serverURL)
			return "myuser", "mypass", nil
		},
	}

	username, password, err := h.Get("registry.example.com")
	require.NoError(t, err)
	assert.Equal(t, "myuser", username)
	assert.Equal(t, "mypass", password)
}

func TestCredentialHelper_Get_Error(t *testing.T) {
	h := &credentialHelper{
		suffix: "bad",
		execFn: func(serverURL string) (string, string, error) {
			return "", "", assert.AnError
		},
	}

	_, _, err := h.Get("registry.example.com")
	assert.Error(t, err)
}

func TestCredentialHelper_Get_TokenAuth(t *testing.T) {
	h := &credentialHelper{
		suffix: "test",
		execFn: func(serverURL string) (string, string, error) {
			return "<token>", "my-identity-token", nil
		},
	}

	username, secret, err := h.Get("registry.example.com")
	require.NoError(t, err)
	assert.Equal(t, "<token>", username)
	assert.Equal(t, "my-identity-token", secret)
}

func TestNewCredentialHelperKeychain(t *testing.T) {
	// We can't test actual credential helper binaries in unit tests,
	// but we can verify the keychain is constructed without error.
	kc := newCredentialHelperKeychain("nonexistent")
	require.NotNil(t, kc)

	// The keychain should implement authn.Keychain
	reg, err := name.NewRegistry("example.com")
	require.NoError(t, err)

	// Resolving against a non-existent helper should return Anonymous (not error)
	// because authn.NewKeychainFromHelper returns Anonymous on helper error.
	auth, err := kc.Resolve(reg)
	require.NoError(t, err)
	assert.Equal(t, authn.Anonymous, auth)
}

func TestCredentialHelper_Priority(t *testing.T) {
	// Test that explicit credentials take priority
	c := ToRegistry("gcr.io/proj/app:v1",
		WithCredentials("explicit-user", "explicit-pass"),
		WithCredentialHelper("gcr"),
	)
	assert.Equal(t, "explicit-user", c.username)
	assert.Equal(t, "explicit-pass", c.password)
	assert.Equal(t, "gcr", c.credentialHelper)
	// The actual priority is enforced in writeRegistry() — explicit creds > helper > default
}
