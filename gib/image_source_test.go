package gib

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pushBase pushes a base image to ref and returns its digest.
func pushBase(t *testing.T, ref string, opts ...remote.Option) string {
	t.Helper()
	img, err := random.Image(64, 1)
	require.NoError(t, err)
	r, err := name.ParseReference(ref)
	require.NoError(t, err)
	require.NoError(t, remote.Write(r, img, opts...))
	d, err := img.Digest()
	require.NoError(t, err)
	return d.String()
}

func toTar(t *testing.T, opts ...ContainerizerOption) *Containerizer {
	t.Helper()
	return ToTar(filepath.Join(t.TempDir(), "image.tar"), append(opts, WithTarImageName("x"), WithAllowInsecureRegistries(true))...)
}

func TestRegistrySource_PullsWithoutCredentialsFirst(t *testing.T) {
	isolated(t)
	ref := serve(t) + "/acme/base:1"
	digest := pushBase(t, ref)
	l := &logs{}

	_, err := From(ref, WithSourceCredentialHelper("nonexistent")).OnLog(l.handle).Containerize(context.Background(), toTar(t))

	require.NoError(t, err, "the helper is never run")
	assert.Equal(t, []string{"Using base image with digest: " + digest}, l.at(LevelLifecycle))
	assert.Contains(t, l.at(LevelProgress), "Getting manifest for base image "+ref+"...")
}

func TestRegistrySource_AskedForCredentialsTriesAgainWithThem(t *testing.T) {
	isolated(t)
	host := serveWithAuth(t, "u", "p")
	ref := host + "/acme/base:1"
	pushBase(t, ref, remote.WithAuth(&authn.Basic{Username: "u", Password: "p"}))
	l := &logs{}

	_, err := From(ref, WithSourceCredential(Credential{"u", "p"}, "--from-username/--from-password")).OnLog(l.handle).
		Containerize(context.Background(), toTar(t, WithSendCredentialsOverHTTP(true)))

	require.NoError(t, err)
	assert.Equal(t, []string{
		"The base image requires auth. Trying again for " + ref + "...",
		"Using credentials from --from-username/--from-password for " + ref,
	}, l.at(LevelLifecycle)[:2])
}

func TestRegistrySource_PullsFromAMirrorFirst(t *testing.T) {
	isolated(t)
	origin, mirror := serve(t), serve(t)
	pushBase(t, mirror+"/acme/base:1")
	l := &logs{}

	_, err := From(origin+"/acme/base:1").OnLog(l.handle).
		Containerize(context.Background(), toTar(t, WithRegistryMirrors(origin, "unreachable.invalid", mirror)))

	require.NoError(t, err, "the origin has no such image")
	assert.Equal(t, []string{
		"trying mirror unreachable.invalid for the base image",
		"trying mirror " + mirror + " for the base image",
		"pulled manifest from mirror " + mirror,
	}, l.at(LevelInfo)[1:])
}
