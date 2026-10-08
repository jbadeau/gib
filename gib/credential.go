package gib

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/go-containerregistry/pkg/authn"
	"golang.org/x/oauth2/google"
)

// Credential holds registry authentication credentials.
type Credential struct {
	Username string
	Password string
}

// tokenUser is the username of a credential whose password is an OAuth2
// refresh token, Jib's Credential.OAUTH2_TOKEN_USER_NAME.
const tokenUser = "<token>"

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

// retriever returns a credential, or nil when it has none for the
// registry.
type retriever func() (*Credential, error)

// retrievers are the sources of ref's credentials, tried in order until
// one has some, as Jib's DefaultCredentialRetrievers lists them.
func (c credentials) retrievers(ref reference, log LogHandler) []retriever {
	got := func(source string) {
		log.log(LevelLifecycle, "Using %s for %s", source, ref)
	}
	var rs []retriever
	if c.known != nil {
		rs = append(rs, func() (*Credential, error) {
			got("credentials from " + c.knownSource)
			return c.known, nil
		})
	}
	if c.helper != "" {
		helper := c.helper
		if !strings.ContainsRune(helper, filepath.Separator) {
			helper = "docker-credential-" + helper
		}
		rs = append(rs, func() (*Credential, error) {
			cred, err := runHelper(helper, ref.registry)
			var unhandled *unhandledError
			if errors.As(err, &unhandled) {
				log.log(LevelInfo, "No credentials for %s in %s", ref.registry, helper)
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			got("credential helper " + filepath.Base(helper))
			return cred, nil
		})
	}
	for _, file := range dockerConfigFiles() {
		rs = append(rs, func() (*Credential, error) {
			cred, err := fromDockerConfig(file, ref.registry, log)
			if err != nil {
				log.log(LevelInfo, "Unable to parse Docker config file: %s", file)
				return nil, nil
			}
			if cred != nil {
				got("credentials from Docker config (" + file + ")")
			}
			return cred, nil
		})
	}
	rs = append(rs, func() (*Credential, error) {
		for _, w := range wellKnownHelpers {
			if !strings.HasSuffix(ref.registry, w.suffix) {
				continue
			}
			cred, err := runHelper(w.helper, ref.registry)
			var missing *HelperMissingError
			var unhandled *unhandledError
			if errors.As(err, &missing) || errors.As(err, &unhandled) {
				log.log(LevelInfo, "%s", err)
				if c := errors.Unwrap(err); c != nil {
					log.log(LevelInfo, "  Caused by: %s", c)
				}
				continue
			}
			if err != nil {
				return nil, err
			}
			got("credential helper " + w.helper)
			return cred, nil
		}
		return nil, nil
	})
	rs = append(rs, func() (*Credential, error) {
		return googleADC(ref, log, got), nil
	})
	return rs
}

// retrieve is the first credential one of rs has, or nil.
func retrieve(rs []retriever) (*Credential, error) {
	for _, r := range rs {
		cred, err := r()
		if err != nil || cred != nil {
			return cred, err
		}
	}
	return nil, nil
}

// keychain resolves every registry to the credential its retrievers
// find, looked for once.
type keychain struct {
	once sync.Once
	rs   []retriever
	auth authn.Authenticator
	err  error
}

func (k *keychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	k.once.Do(func() {
		cred, err := retrieve(k.rs)
		switch {
		case err != nil:
			k.err = err
		case cred == nil:
			k.auth = authn.Anonymous
		case cred.Username == tokenUser:
			k.auth = authn.FromConfig(authn.AuthConfig{IdentityToken: cred.Password})
		default:
			k.auth = &authn.Basic{Username: cred.Username, Password: cred.Password}
		}
	})
	return k.auth, k.err
}

var wellKnownHelpers = []struct{ suffix, helper string }{
	{"gcr.io", "docker-credential-gcr"},
	{"amazonaws.com", "docker-credential-ecr-login"},
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

// unhandledError is a credential helper that has nothing for a server.
type unhandledError struct{ helper, server, output string }

func (e *unhandledError) Error() string {
	return "The credential helper (" + e.helper + ") has nothing for server URL: " + e.server + "\n\nGot output:\n\n" + e.output
}

// runHelper asks a Docker credential helper for server's credentials as
// Jib's DockerCredentialHelper does: the server on its stdin, a
// Username and Secret on its stdout, and nothing usable there meaning it
// has none.
func runHelper(helper, server string) (*Credential, error) {
	cmd := exec.Command(helper, "get")
	cmd.Stdin = strings.NewReader(server)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return nil, &HelperMissingError{Helper: helper, Cause: err}
		}
		return nil, err
	}
	_ = cmd.Wait()
	out := stdout.String()
	if strings.Contains(out, "credentials not found in native keychain") {
		return nil, &unhandledError{helper, server, out}
	}
	if out == "" {
		return nil, &unhandledError{helper, server, stderr.String()}
	}
	var resp struct {
		Username string `json:"Username"`
		Secret   string `json:"Secret"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil || resp.Username == "" || resp.Secret == "" {
		return nil, &unhandledError{helper, server, out}
	}
	return &Credential{Username: resp.Username, Password: resp.Secret}, nil
}

// dockerConfigFiles are the files Jib reads credentials from, in order:
// Podman's auth.json, then Docker's config.json, Kubernetes'
// .dockerconfigjson and the legacy .dockercfg.
func dockerConfigFiles() []string {
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
	if d, ok := os.LookupEnv("HOME"); ok {
		add(filepath.Join(d, ".config", auth))
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
	if d, ok := os.LookupEnv("HOME"); ok {
		docker(filepath.Join(d, ".docker"))
	}
	return files
}

type dockerAuth struct {
	Auth          *string
	Username      *string
	Password      *string
	IdentityToken *string
}

type dockerConfig struct {
	Auths       map[string]dockerAuth
	CredsStore  string
	CredHelpers map[string]string
}

// registryAliases are registry and the names it goes by, registry first.
func registryAliases(registry string) []string {
	hub := []string{"registry.hub.docker.com", "index.docker.io", "registry-1.docker.io", "docker.io"}
	for _, h := range hub {
		if h == registry {
			out := []string{registry}
			for _, a := range hub {
				if a != registry {
					out = append(out, a)
				}
			}
			return out
		}
	}
	return []string{registry}
}

// matchKey is the first of keys naming registry, by Jib's matchers in
// their order: exactly, with https://, with a path, with both.
func matchKey(keys []string, registry string) (string, bool) {
	matchers := []func(string) bool{
		func(k string) bool { return k == registry },
		func(k string) bool { return k == "https://"+registry },
		func(k string) bool { return strings.HasPrefix(k, registry+"/") },
		func(k string) bool { return strings.HasPrefix(k, "https://"+registry+"/") },
	}
	for _, m := range matchers {
		for _, k := range keys {
			if m(k) {
				return k, true
			}
		}
	}
	return "", false
}

// fromDockerConfig is registry's credential in a Docker config file, as
// Jib's DockerConfigCredentialRetriever reads it: for each of the
// registry's aliases, its credential helper or the config's credsStore,
// then its auths entry.
func fromDockerConfig(file, registry string, log LogHandler) (*Credential, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg dockerConfig
	if filepath.Base(file) == ".dockercfg" {
		err = json.Unmarshal(data, &cfg.Auths)
	} else {
		err = json.Unmarshal(data, &cfg)
	}
	if err != nil {
		return nil, err
	}
	var authKeys, helperKeys []string
	for k := range cfg.Auths {
		authKeys = append(authKeys, k)
	}
	for k := range cfg.CredHelpers {
		helperKeys = append(helperKeys, k)
	}
	for _, alias := range registryAliases(registry) {
		helper, server := "", alias
		if k, ok := matchKey(helperKeys, alias); ok {
			helper, server = "docker-credential-"+cfg.CredHelpers[k], k
		} else if cfg.CredsStore != "" {
			helper = "docker-credential-" + cfg.CredsStore
		}
		if helper != "" {
			log.log(LevelInfo, "trying %s for %s", helper, alias)
			cred, err := runHelper(helper, server)
			if err == nil {
				return cred, nil
			}
			log.log(LevelWarn, "%s", err)
			if c := errors.Unwrap(err); c != nil {
				log.log(LevelWarn, "  Caused by: %s", c)
			}
		}
		k, ok := matchKey(authKeys, alias)
		if !ok {
			continue
		}
		a := cfg.Auths[k]
		switch {
		case a.Auth != nil:
			decoded, err := base64.StdEncoding.DecodeString(*a.Auth)
			if err != nil {
				return nil, err
			}
			user, pass, ok := strings.Cut(string(decoded), ":")
			if !ok {
				return nil, fmt.Errorf("auth for %s is not username:password", k)
			}
			log.log(LevelInfo, "Docker config auths section defines credentials for %s", alias)
			if a.IdentityToken != nil && user == "00000000-0000-0000-0000-000000000000" && pass == "" {
				log.log(LevelInfo, "Using 'identityToken' in Docker config auth for %s", alias)
				return &Credential{Username: tokenUser, Password: *a.IdentityToken}, nil
			}
			return &Credential{Username: user, Password: pass}, nil
		case a.Username != nil && a.Password != nil:
			log.log(LevelInfo, "Docker config auths section defines username and password for %s", alias)
			return &Credential{Username: *a.Username, Password: *a.Password}, nil
		}
	}
	return nil, nil
}

// googleADC is an access token from Google's Application Default
// Credentials for a Google registry, as Jib takes one: a service
// account scoped to read and write Cloud Storage.
func googleADC(ref reference, log LogHandler, got func(string)) *Credential {
	if !strings.HasSuffix(ref.registry, "gcr.io") && !strings.HasSuffix(ref.registry, "docker.pkg.dev") {
		return nil
	}
	creds, err := google.FindDefaultCredentials(context.Background(), "https://www.googleapis.com/auth/devstorage.read_write")
	if err != nil {
		log.log(LevelInfo, "ADC not present or error fetching access token: %s", err)
		return nil
	}
	log.log(LevelInfo, "Google ADC found")
	var kind struct{ Type string }
	if json.Unmarshal(creds.JSON, &kind) == nil && kind.Type == "service_account" {
		log.log(LevelInfo, "ADC is a service account. Setting GCS read-write scope")
	}
	tok, err := creds.TokenSource.Token()
	if err != nil {
		log.log(LevelInfo, "ADC not present or error fetching access token: %s", err)
		return nil
	}
	got("Google Application Default Credentials")
	return &Credential{Username: "oauth2accesstoken", Password: tok.AccessToken}
}
