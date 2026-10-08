package gib

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var exampleRef = reference{registry: "registry.example.com", repository: "acme/app", tag: "latest"}

// helperScript writes an executable credential helper that prints out
// and exits with code, and returns its path.
func helperScript(t *testing.T, dir, name, out string, code int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	script := fmt.Sprintf("#!/bin/sh\ncat > /dev/null\necho '%s'\nexit %d\n", out, code)
	require.NoError(t, os.WriteFile(p, []byte(script), 0o755))
	return p
}

// onPath puts dir first on the PATH.
func onPath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeFile(t *testing.T, file, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
}

func basic(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

// resolved is the credential c's keychain gives ref's registry.
func resolved(t *testing.T, c credentials, ref reference, log LogHandler) (*authn.AuthConfig, error) {
	t.Helper()
	reg, err := name.NewRegistry(ref.registry)
	require.NoError(t, err)
	if ref.registry == dockerHub {
		reg, err = name.NewRegistry(name.DefaultRegistry)
		require.NoError(t, err)
	}
	a, err := c.keychain(ref, log).Resolve(reg)
	if err != nil {
		return nil, err
	}
	if a == authn.Anonymous {
		return nil, nil
	}
	return a.Authorization()
}

func TestCredentials_AKnownCredentialComesBeforeTheHelper(t *testing.T) {
	isolated(t)
	l := &logs{}
	c := credentials{known: &Credential{"u", "p"}, knownSource: "--username/--password", helper: "nonexistent"}

	cfg, err := resolved(t, c, exampleRef, l.handle)

	require.NoError(t, err)
	assert.Equal(t, &authn.AuthConfig{Username: "u", Password: "p"}, cfg)
	assert.Equal(t, []string{"Using credentials from --username/--password for registry.example.com/acme/app"}, l.at(LevelLifecycle))
}

func TestCredentials_AHelperNamedByAPathIsRun(t *testing.T) {
	isolated(t)
	helper := helperScript(t, t.TempDir(), "my-helper", `{"Username":"hu","Secret":"hs"}`, 0)
	l := &logs{}

	cfg, err := resolved(t, credentials{helper: helper}, exampleRef, l.handle)

	require.NoError(t, err)
	assert.Equal(t, &authn.AuthConfig{Username: "hu", Password: "hs"}, cfg)
	assert.Equal(t, []string{"Using credential helper my-helper for registry.example.com/acme/app"}, l.at(LevelLifecycle))
}

func TestCredentials_AHelperSuffixNamesADockerCredentialHelper(t *testing.T) {
	isolated(t)
	dir := t.TempDir()
	helperScript(t, dir, "docker-credential-acme", `{"Username":"hu","Secret":"hs"}`, 0)
	onPath(t, dir)

	cfg, err := resolved(t, credentials{helper: "acme"}, exampleRef, nil)

	require.NoError(t, err)
	assert.Equal(t, &authn.AuthConfig{Username: "hu", Password: "hs"}, cfg)
}

func TestCredentials_AHelperPathThatDoesNotExistIsRefused(t *testing.T) {
	assert.EqualError(t, credentials{helper: "./nope/helper"}.check(), "Specified credential helper was not found: ./nope/helper")
	assert.NoError(t, credentials{helper: "nope"}.check(), "a suffix is looked for only when it is run")
}

func TestCredentials_AHelperThatIsNotInstalledFails(t *testing.T) {
	isolated(t)

	_, err := resolved(t, credentials{helper: "nonexistent"}, exampleRef, nil)

	assert.EqualError(t, err, "The system does not have docker-credential-nonexistent CLI")
}

func TestCredentials_AHelperWithNothingForTheRegistryIsPassedOver(t *testing.T) {
	isolated(t)
	helper := helperScript(t, t.TempDir(), "helper", "credentials not found in native keychain", 1)
	l := &logs{}

	cfg, err := resolved(t, credentials{helper: helper}, exampleRef, l.handle)

	require.NoError(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, l.at(LevelInfo), "No credentials for registry.example.com in "+helper)
}

func TestCredentials_DockerConfigAuthsAreRead(t *testing.T) {
	home := isolated(t)
	config := filepath.Join(home, ".docker", "config.json")
	writeFile(t, config, `{"auths":{"https://registry.example.com/v1/":{"auth":"`+basic("du", "dp")+`"}}}`)
	l := &logs{}

	cfg, err := resolved(t, credentials{}, exampleRef, l.handle)

	require.NoError(t, err)
	assert.Equal(t, &authn.AuthConfig{Username: "du", Password: "dp"}, cfg)
	assert.Equal(t, []string{"Using credentials from Docker config (" + config + ") for registry.example.com/acme/app"}, l.at(LevelLifecycle))
}

func TestCredentials_PodmansAuthComesBeforeDockersConfig(t *testing.T) {
	home := isolated(t)
	runtime := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	writeFile(t, filepath.Join(runtime, "containers", "auth.json"), `{"auths":{"registry.example.com":{"auth":"`+basic("pu", "pp")+`"}}}`)
	writeFile(t, filepath.Join(home, ".docker", "config.json"), `{"auths":{"registry.example.com":{"auth":"`+basic("du", "dp")+`"}}}`)

	cfg, err := resolved(t, credentials{}, exampleRef, nil)

	require.NoError(t, err)
	assert.Equal(t, &authn.AuthConfig{Username: "pu", Password: "pp"}, cfg)
}

func TestCredentials_ADockerConfigCredentialHelperComesBeforeItsAuths(t *testing.T) {
	home := isolated(t)
	dir := t.TempDir()
	helperScript(t, dir, "docker-credential-acme", `{"Username":"hu","Secret":"hs"}`, 0)
	onPath(t, dir)
	writeFile(t, filepath.Join(home, ".docker", "config.json"),
		`{"credHelpers":{"registry.example.com":"acme"},"auths":{"registry.example.com":{"auth":"`+basic("du", "dp")+`"}}}`)

	cfg, err := resolved(t, credentials{}, exampleRef, nil)

	require.NoError(t, err)
	assert.Equal(t, &authn.AuthConfig{Username: "hu", Password: "hs"}, cfg)
}

// Docker reads a config whose credsStore has nothing for a registry as
// having no credentials for it; Jib would fall back to the auths.
func TestCredentials_ACredsStoreWithNothingHasNoCredentials(t *testing.T) {
	home := isolated(t)
	dir := t.TempDir()
	helperScript(t, dir, "docker-credential-store", "credentials not found in native keychain", 1)
	onPath(t, dir)
	writeFile(t, filepath.Join(home, ".docker", "config.json"),
		`{"credsStore":"store","auths":{"registry.example.com":{"auth":"`+basic("du", "dp")+`"}}}`)

	cfg, err := resolved(t, credentials{}, exampleRef, nil)

	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestCredentials_AFailingCredsStoreIsPassedOverWithAWarning(t *testing.T) {
	home := isolated(t)
	writeFile(t, filepath.Join(home, ".docker", "config.json"), `{"credsStore":"nonexistent"}`)
	l := &logs{}

	cfg, err := resolved(t, credentials{}, exampleRef, l.handle)

	require.NoError(t, err)
	assert.Nil(t, cfg)
	assert.Len(t, l.at(LevelWarn), 1)
}

func TestCredentials_DockerHubIsKeptUnderItsV1URL(t *testing.T) {
	home := isolated(t)
	writeFile(t, filepath.Join(home, ".docker", "config.json"), `{"auths":{"https://index.docker.io/v1/":{"auth":"`+basic("hub", "hp")+`"}}}`)

	cfg, err := resolved(t, credentials{}, mustParse(t, "acme/app"), nil)

	require.NoError(t, err)
	assert.Equal(t, &authn.AuthConfig{Username: "hub", Password: "hp"}, cfg)
}

func TestCredentials_TheLegacyDockercfgIsRead(t *testing.T) {
	home := isolated(t)
	writeFile(t, filepath.Join(home, ".docker", ".dockercfg"), `{"registry.example.com":{"auth":"`+basic("lu", "lp")+`"}}`)

	cfg, err := resolved(t, credentials{}, exampleRef, nil)

	require.NoError(t, err)
	assert.Equal(t, &authn.AuthConfig{Username: "lu", Password: "lp"}, cfg)
}

func TestCredentials_AKubernetesDockerconfigjsonIsRead(t *testing.T) {
	home := isolated(t)
	writeFile(t, filepath.Join(home, ".docker", ".dockerconfigjson"), `{"auths":{"registry.example.com":{"auth":"`+basic("ku", "kp")+`"}}}`)

	cfg, err := resolved(t, credentials{}, exampleRef, nil)

	require.NoError(t, err)
	assert.Equal(t, &authn.AuthConfig{Username: "ku", Password: "kp"}, cfg)
}

func TestCredentials_AWellKnownHelperThatIsNotInstalledIsPassedOver(t *testing.T) {
	isolated(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "none.json"))
	l := &logs{}

	cfg, err := resolved(t, credentials{}, mustParse(t, "gcr.io/p/i"), l.handle)

	require.NoError(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, l.at(LevelInfo), "The system does not have docker-credential-gcr CLI")
}
