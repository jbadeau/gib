package gib

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var exampleRef = reference{registry: "registry.example.com", repository: "acme/app", tag: "latest"}

// helperScript writes an executable credential helper that prints out
// for any server, and returns its path.
func helperScript(t *testing.T, dir, name, out string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\ncat > /dev/null\necho '"+out+"'\n"), 0o755))
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

func TestCredentials_AKnownCredentialComesBeforeTheHelper(t *testing.T) {
	isolated(t)
	l := &logs{}
	c := credentials{known: &Credential{"u", "p"}, knownSource: "--username/--password", helper: "nonexistent"}

	cred, err := retrieve(c.retrievers(exampleRef, l.handle))

	require.NoError(t, err)
	assert.Equal(t, &Credential{"u", "p"}, cred)
	assert.Equal(t, []string{"Using credentials from --username/--password for registry.example.com/acme/app"}, l.at(LevelLifecycle))
}

func TestCredentials_AHelperNamedByAPathIsRun(t *testing.T) {
	isolated(t)
	helper := helperScript(t, t.TempDir(), "my-helper", `{"Username":"hu","Secret":"hs"}`)
	l := &logs{}

	cred, err := retrieve(credentials{helper: helper}.retrievers(exampleRef, l.handle))

	require.NoError(t, err)
	assert.Equal(t, &Credential{"hu", "hs"}, cred)
	assert.Equal(t, []string{"Using credential helper my-helper for registry.example.com/acme/app"}, l.at(LevelLifecycle))
}

func TestCredentials_AHelperSuffixNamesADockerCredentialHelper(t *testing.T) {
	isolated(t)
	dir := t.TempDir()
	helperScript(t, dir, "docker-credential-acme", `{"Username":"hu","Secret":"hs"}`)
	onPath(t, dir)

	cred, err := retrieve(credentials{helper: "acme"}.retrievers(exampleRef, nil))

	require.NoError(t, err)
	assert.Equal(t, &Credential{"hu", "hs"}, cred)
}

func TestCredentials_AHelperPathThatDoesNotExistIsRefused(t *testing.T) {
	assert.EqualError(t, credentials{helper: "./nope/helper"}.check(), "Specified credential helper was not found: ./nope/helper")
	assert.NoError(t, credentials{helper: "nope"}.check(), "a suffix is looked for only when it is run")
}

func TestCredentials_AHelperThatIsNotInstalledFails(t *testing.T) {
	isolated(t)

	_, err := retrieve(credentials{helper: "nonexistent"}.retrievers(exampleRef, nil))

	assert.EqualError(t, err, "The system does not have docker-credential-nonexistent CLI")
}

func TestCredentials_AHelperWithNothingForTheRegistryIsPassedOver(t *testing.T) {
	isolated(t)
	helper := helperScript(t, t.TempDir(), "helper", "credentials not found in native keychain")
	l := &logs{}

	cred, err := retrieve(credentials{helper: helper}.retrievers(exampleRef, l.handle))

	require.NoError(t, err)
	assert.Nil(t, cred)
	assert.Contains(t, l.at(LevelInfo), "No credentials for registry.example.com in "+helper)
}

func TestCredentials_DockerConfigAuthsAreRead(t *testing.T) {
	home := isolated(t)
	config := filepath.Join(home, ".docker", "config.json")
	writeFile(t, config, `{"auths":{"https://registry.example.com/v1/":{"auth":"`+basic("du", "dp")+`"}}}`)
	l := &logs{}

	cred, err := retrieve(credentials{}.retrievers(exampleRef, l.handle))

	require.NoError(t, err)
	assert.Equal(t, &Credential{"du", "dp"}, cred)
	assert.Equal(t, []string{"Using credentials from Docker config (" + config + ") for registry.example.com/acme/app"}, l.at(LevelLifecycle))
}

func TestCredentials_PodmansAuthComesBeforeDockersConfig(t *testing.T) {
	home := isolated(t)
	runtime := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	writeFile(t, filepath.Join(runtime, "containers", "auth.json"), `{"auths":{"registry.example.com":{"auth":"`+basic("pu", "pp")+`"}}}`)
	writeFile(t, filepath.Join(home, ".docker", "config.json"), `{"auths":{"registry.example.com":{"auth":"`+basic("du", "dp")+`"}}}`)

	cred, err := retrieve(credentials{}.retrievers(exampleRef, nil))

	require.NoError(t, err)
	assert.Equal(t, &Credential{"pu", "pp"}, cred)
}

func TestCredentials_ADockerConfigCredentialHelperComesBeforeItsAuths(t *testing.T) {
	home := isolated(t)
	dir := t.TempDir()
	helperScript(t, dir, "docker-credential-acme", `{"Username":"hu","Secret":"hs"}`)
	onPath(t, dir)
	writeFile(t, filepath.Join(home, ".docker", "config.json"),
		`{"credHelpers":{"registry.example.com":"acme"},"auths":{"registry.example.com":{"auth":"`+basic("du", "dp")+`"}}}`)

	cred, err := retrieve(credentials{}.retrievers(exampleRef, nil))

	require.NoError(t, err)
	assert.Equal(t, &Credential{"hu", "hs"}, cred)
}

func TestCredentials_ACredsStoreWithNothingFallsBackToAuths(t *testing.T) {
	home := isolated(t)
	dir := t.TempDir()
	helperScript(t, dir, "docker-credential-store", "credentials not found in native keychain")
	onPath(t, dir)
	writeFile(t, filepath.Join(home, ".docker", "config.json"),
		`{"credsStore":"store","auths":{"registry.example.com":{"username":"du","password":"dp"}}}`)
	l := &logs{}

	cred, err := retrieve(credentials{}.retrievers(exampleRef, l.handle))

	require.NoError(t, err)
	assert.Equal(t, &Credential{"du", "dp"}, cred)
	assert.Equal(t, []string{"The credential helper (docker-credential-store) has nothing for server URL: registry.example.com\n\nGot output:\n\ncredentials not found in native keychain\n"}, l.at(LevelWarn))
}

func TestCredentials_DockerHubGoesByItsAliases(t *testing.T) {
	home := isolated(t)
	writeFile(t, filepath.Join(home, ".docker", "config.json"), `{"auths":{"https://index.docker.io/v1/":{"auth":"`+basic("hub", "hp")+`"}}}`)

	cred, err := retrieve(credentials{}.retrievers(mustParse(t, "acme/app"), nil))

	require.NoError(t, err)
	assert.Equal(t, &Credential{"hub", "hp"}, cred)
}

func TestCredentials_TheLegacyDockercfgIsRead(t *testing.T) {
	home := isolated(t)
	writeFile(t, filepath.Join(home, ".docker", ".dockercfg"), `{"registry.example.com":{"auth":"`+basic("lu", "lp")+`"}}`)

	cred, err := retrieve(credentials{}.retrievers(exampleRef, nil))

	require.NoError(t, err)
	assert.Equal(t, &Credential{"lu", "lp"}, cred)
}

func TestCredentials_AnAzureIdentityTokenIsARefreshToken(t *testing.T) {
	home := isolated(t)
	writeFile(t, filepath.Join(home, ".docker", "config.json"),
		`{"auths":{"registry.example.com":{"auth":"`+basic("00000000-0000-0000-0000-000000000000", "")+`","identitytoken":"tok"}}}`)

	cred, err := retrieve(credentials{}.retrievers(exampleRef, nil))

	require.NoError(t, err)
	assert.Equal(t, &Credential{tokenUser, "tok"}, cred)
}

func TestCredentials_AWellKnownHelperThatIsNotInstalledIsPassedOver(t *testing.T) {
	isolated(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "none.json"))
	l := &logs{}

	cred, err := retrieve(credentials{}.retrievers(mustParse(t, "gcr.io/p/i"), l.handle))

	require.NoError(t, err)
	assert.Nil(t, cred)
	assert.Contains(t, l.at(LevelInfo), "The system does not have docker-credential-gcr CLI")
}
