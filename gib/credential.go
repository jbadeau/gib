package gib

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/docker-credential-helpers/client"
	helpers "github.com/docker/docker-credential-helpers/credentials"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/google"
)

// Credential holds registry authentication credentials.
type Credential struct {
	Username string
	Password string
}

// credentials are where a registry's credentials come from: a known
// credential and a credential helper, each set by the user, then the
// sources Jib's DefaultCredentialRetrievers tries by default.
type credentials struct {
	known       *Credential
	knownSource string
	helper      string
}

// HelperNotFoundError is a credential helper named by its path that does
// not exist.
type HelperNotFoundError struct{ Helper string }

func (e *HelperNotFoundError) Error() string {
	return "Specified credential helper was not found: " + e.Helper
}

// HelperMissingError is a credential helper that is not installed.
type HelperMissingError struct {
	Helper string
	Cause  error
}

func (e *HelperMissingError) Error() string {
	return "The system does not have " + e.Helper + " CLI"
}

func (e *HelperMissingError) Unwrap() error { return e.Cause }

// check fails for a credential helper named by a path that does not
// exist, as Jib fails before it builds anything.
func (c credentials) check() error {
	if c.helper == "" || !strings.ContainsRune(c.helper, filepath.Separator) {
		return nil
	}
	if _, err := os.Stat(c.helper); err != nil {
		return &HelperNotFoundError{c.helper}
	}
	return nil
}

// keychain is where ref's credentials are looked for, in the order of
// Jib's DefaultCredentialRetrievers: the known credential, the credential
// helper, Podman's and Docker's config files, the well-known credential
// helpers of Google's and Amazon's registries, and Google's credentials.
// The first source with a credential gives it, looked for once.
func (c credentials) keychain(ref reference, log LogHandler) authn.Keychain {
	got := func(source string) func() {
		return func() { log.log(LevelLifecycle, "Using %s for %s", source, ref) }
	}
	var kcs []authn.Keychain
	if c.known != nil {
		kcs = append(kcs, logged{static{authn.FromConfig(authn.AuthConfig{Username: c.known.Username, Password: c.known.Password})},
			got("credentials from " + c.knownSource)})
	}
	if c.helper != "" {
		h := c.helper
		if !strings.ContainsRune(h, filepath.Separator) {
			h = "docker-credential-" + h
		}
		kcs = append(kcs, logged{helper{program: h, required: true, log: log}, got("credential helper " + filepath.Base(h))})
	}
	for _, f := range configFiles() {
		kcs = append(kcs, logged{configFile{file: f, log: log}, got("credentials from Docker config (" + f + ")")})
	}
	for _, w := range []struct{ suffix, program string }{
		{"gcr.io", "docker-credential-gcr"},
		{"amazonaws.com", "docker-credential-ecr-login"},
	} {
		if strings.HasSuffix(ref.registry, w.suffix) {
			kcs = append(kcs, logged{helper{program: w.program, log: log}, got("credential helper " + w.program)})
		}
	}
	kcs = append(kcs, logged{google.Keychain, got("Google Application Default Credentials")})
	return &once{kc: authn.NewMultiKeychain(kcs...)}
}

// once resolves its keychain once, for every request a build makes.
type once struct {
	kc   authn.Keychain
	o    sync.Once
	auth authn.Authenticator
	err  error
}

func (k *once) Resolve(r authn.Resource) (authn.Authenticator, error) {
	k.o.Do(func() { k.auth, k.err = k.kc.Resolve(r) })
	return k.auth, k.err
}

// logged logs where a credential came from when its keychain has one.
type logged struct {
	kc  authn.Keychain
	got func()
}

func (l logged) Resolve(r authn.Resource) (authn.Authenticator, error) {
	a, err := l.kc.Resolve(r)
	if err == nil && a != authn.Anonymous {
		l.got()
	}
	return a, err
}

type static struct{ auth authn.Authenticator }

func (s static) Resolve(authn.Resource) (authn.Authenticator, error) { return s.auth, nil }

// serverURL is the name a registry's credentials are kept under: Docker
// Hub's is its v1 URL.
func serverURL(r authn.Resource) string {
	if r.RegistryStr() == name.DefaultRegistry {
		return authn.DefaultAuthKey
	}
	return r.RegistryStr()
}

// helper asks a Docker credential helper for a registry's credentials.
// One the user named fails the build when it is not installed; a
// well-known one is passed over.
type helper struct {
	program  string
	required bool
	log      LogHandler
}

func (h helper) Resolve(r authn.Resource) (authn.Authenticator, error) {
	if _, err := exec.LookPath(h.program); err != nil {
		missing := &HelperMissingError{Helper: h.program, Cause: err}
		if h.required {
			return nil, missing
		}
		h.log.log(LevelInfo, "%s", missing)
		return authn.Anonymous, nil
	}
	creds, err := client.Get(client.NewShellProgramFunc(h.program), serverURL(r))
	switch {
	case helpers.IsErrCredentialsNotFound(err):
		h.log.log(LevelInfo, "No credentials for %s in %s", r.RegistryStr(), h.program)
		return authn.Anonymous, nil
	case err != nil && h.required:
		return nil, err
	case err != nil:
		h.log.log(LevelInfo, "%s", err)
		return authn.Anonymous, nil
	case creds.Username == "<token>":
		return authn.FromConfig(authn.AuthConfig{IdentityToken: creds.Secret}), nil
	}
	return authn.FromConfig(authn.AuthConfig{Username: creds.Username, Password: creds.Secret}), nil
}

// configFile is a registry's credentials in a Docker config file, read
// as Docker reads it: its credential helper, or its credsStore, or its
// auths. A file that cannot be read, or whose helper fails, has none.
type configFile struct {
	file string
	log  LogHandler
}

func (c configFile) Resolve(r authn.Resource) (authn.Authenticator, error) {
	data, err := os.ReadFile(c.file)
	if errors.Is(err, os.ErrNotExist) {
		return authn.Anonymous, nil
	}
	var cf *configfile.ConfigFile
	if err == nil {
		if filepath.Base(c.file) == ".dockercfg" {
			// The legacy file is what a config file's auths holds.
			data = append(append([]byte(`{"auths":`), data...), '}')
		}
		cf, err = config.LoadFromReader(bytes.NewReader(data))
	}
	if err != nil {
		c.log.log(LevelInfo, "Unable to parse Docker config file: %s", c.file)
		return authn.Anonymous, nil
	}
	cfg, err := cf.GetAuthConfig(serverURL(r))
	if err != nil {
		c.log.log(LevelWarn, "%s", err)
		return authn.Anonymous, nil
	}
	if cfg.Username == "" && cfg.Password == "" && cfg.Auth == "" && cfg.IdentityToken == "" && cfg.RegistryToken == "" {
		return authn.Anonymous, nil
	}
	return authn.FromConfig(authn.AuthConfig{
		Username:      cfg.Username,
		Password:      cfg.Password,
		Auth:          cfg.Auth,
		IdentityToken: cfg.IdentityToken,
		RegistryToken: cfg.RegistryToken,
	}), nil
}

// configFiles are the config files Jib reads credentials from, in its
// order: Podman's auth.json, then Docker's config.json, Kubernetes'
// .dockerconfigjson and the legacy .dockercfg.
func configFiles() []string {
	var files []string
	seen := map[string]bool{}
	add := func(f string) {
		if !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	auth := filepath.Join("containers", "auth.json")
	home, _ := os.UserHomeDir()
	if d, ok := os.LookupEnv("XDG_RUNTIME_DIR"); ok {
		add(filepath.Join(d, auth))
	}
	if d, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok {
		add(filepath.Join(d, auth))
	}
	if home != "" {
		add(filepath.Join(home, ".config", auth))
	}
	docker := func(dir string) {
		for _, f := range []string{"config.json", ".dockerconfigjson", ".dockercfg"} {
			add(filepath.Join(dir, f))
		}
	}
	if d, ok := os.LookupEnv("DOCKER_CONFIG"); ok {
		docker(d)
	}
	if home != "" {
		docker(filepath.Join(home, ".docker"))
	}
	return files
}
