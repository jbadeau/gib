package main

import (
	"bytes"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPushCmd_PrintsThePushedDigest(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	img, err := random.Image(128, 1)
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "image.tar")
	tag, _ := name.NewTag("acme/app:latest")
	require.NoError(t, tarball.WriteToFile(file, tag, img))

	cmd := newPushCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--allow-insecure-registries", file, strings.TrimPrefix(srv.URL, "http://") + "/acme/app:1.4.0"})
	require.NoError(t, cmd.Execute())

	want, _ := img.Digest()
	assert.Equal(t, want.String()+"\n", out.String())
}

func TestPushCmd_TakesATarballAndAReference(t *testing.T) {
	cmd := newPushCmd()
	cmd.SetArgs([]string{"image.tar"})
	require.Error(t, cmd.Execute())
}

func TestPushCmd_RefusesPlainHTTPUnlessInsecureRegistriesAreAllowed(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	img, err := random.Image(128, 1)
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "image.tar")
	tag, _ := name.NewTag("acme/app:latest")
	require.NoError(t, tarball.WriteToFile(file, tag, img))

	cmd := newPushCmd()
	cmd.SetArgs([]string{file, strings.TrimPrefix(srv.URL, "http://") + "/acme/app:1.4.0"})
	assert.ErrorContains(t, cmd.Execute(), "because only secure connections are allowed")
}
