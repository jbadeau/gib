package gib

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const someDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

func TestReference_IsParsedAndPrintedAsJibDoes(t *testing.T) {
	for _, tc := range []struct {
		in, registry, repository, printed, qualified string
	}{
		{"test/x", "registry-1.docker.io", "test/x", "test/x", "test/x:latest"},
		{"busybox", "registry-1.docker.io", "library/busybox", "busybox", "busybox:latest"},
		{"docker.io/library/busybox:1", "registry-1.docker.io", "library/busybox", "busybox:1", "busybox:1"},
		{"docker.io/busybox", "registry-1.docker.io", "busybox", "busybox", "busybox:latest"},
		{"localhost/x", "localhost", "x", "localhost/x", "localhost/x:latest"},
		{"localhost:5000/a/b:v1", "localhost:5000", "a/b", "localhost:5000/a/b:v1", "localhost:5000/a/b:v1"},
		{"gcr.io/p/i:latest", "gcr.io", "p/i", "gcr.io/p/i", "gcr.io/p/i:latest"},
		{"x:1@" + someDigest, "registry-1.docker.io", "library/x", "x:1@" + someDigest, "x@" + someDigest},
	} {
		t.Run(tc.in, func(t *testing.T) {
			r, err := parseReference(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.registry, r.registry)
			assert.Equal(t, tc.repository, r.repository)
			assert.Equal(t, tc.printed, r.String())
			assert.Equal(t, tc.qualified, r.withQualifierString())
		})
	}
}

func TestReference_JibRefusesWhatItDoesNotParse(t *testing.T) {
	for _, in := range []string{"BAD", "Bad Name", "a/b:", "gcr.io/p/i@sha256:12"} {
		_, err := parseReference(in)
		assert.EqualError(t, err, "Invalid image reference: "+in)
	}
}

func TestReference_AnotherTagKeepsItsDigest(t *testing.T) {
	r, err := parseReference("x:1@" + someDigest)
	require.NoError(t, err)
	assert.Equal(t, "x@"+someDigest, r.withQualifier("a").withQualifierString())
	assert.Equal(t, "x:a", mustParse(t, "x").withQualifier("a").withQualifierString())
}

func mustParse(t *testing.T, s string) reference {
	t.Helper()
	r, err := parseReference(s)
	require.NoError(t, err)
	return r
}
