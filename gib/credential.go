package gib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/google/go-containerregistry/pkg/authn"
)

// Credential holds registry authentication credentials.
type Credential struct {
	Username string
	Password string
}

// CredentialRetriever is a function that returns a Credential and whether it was found.
type CredentialRetriever func() (Credential, bool, error)

// credentialHelperResponse is the JSON structure returned by docker-credential-* helpers.
type credentialHelperResponse struct {
	Username string `json:"Username"`
	Secret   string `json:"Secret"`
}

// credentialHelper implements authn.Helper by shelling out to a Docker credential helper binary.
type credentialHelper struct {
	suffix string
	// execFn is injectable for testing; defaults to running docker-credential-<suffix>.
	execFn func(serverURL string) (string, string, error)
}

// Get implements authn.Helper.
func (h *credentialHelper) Get(serverURL string) (string, string, error) {
	return h.execFn(serverURL)
}

// execCredentialHelper runs docker-credential-<suffix> get and parses the response.
func execCredentialHelper(suffix, serverURL string) (string, string, error) {
	cmd := exec.Command("docker-credential-"+suffix, "get")
	cmd.Stdin = bytes.NewBufferString(serverURL)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("credential helper %q failed: %w: %s", suffix, err, stderr.String())
	}

	var resp credentialHelperResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return "", "", fmt.Errorf("parsing credential helper response: %w", err)
	}

	return resp.Username, resp.Secret, nil
}

// newCredentialHelperKeychain creates an authn.Keychain that uses a Docker credential helper binary.
func newCredentialHelperKeychain(suffix string) authn.Keychain {
	h := &credentialHelper{
		suffix: suffix,
		execFn: func(serverURL string) (string, string, error) {
			return execCredentialHelper(suffix, serverURL)
		},
	}
	return authn.NewKeychainFromHelper(h)
}
