package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCmd_RequiresTarget(t *testing.T) {
	cmd := newBuildCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target")
}

func TestBuildCmd_FlagParsing(t *testing.T) {
	cmd := newBuildCmd()
	cmd.SetArgs([]string{
		"--target", "gcr.io/proj/app:v1",
		"--build-file", "custom.yaml",
		"--context", "/src",
		"--from", "alpine:3.18",
		"--image-format", "OCI",
		"--user", "app",
		"--verbosity", "debug",
		"--allow-insecure-registries",
	})

	// Parse flags without executing
	err := cmd.ParseFlags([]string{
		"--target", "gcr.io/proj/app:v1",
		"--build-file", "custom.yaml",
		"--context", "/src",
		"--from", "alpine:3.18",
		"--image-format", "OCI",
		"--user", "app",
		"--verbosity", "debug",
		"--allow-insecure-registries",
	})
	require.NoError(t, err)

	target, _ := cmd.Flags().GetString("target")
	assert.Equal(t, "gcr.io/proj/app:v1", target)

	bf, _ := cmd.Flags().GetString("build-file")
	assert.Equal(t, "custom.yaml", bf)

	ctx, _ := cmd.Flags().GetString("context")
	assert.Equal(t, "/src", ctx)

	from, _ := cmd.Flags().GetString("from")
	assert.Equal(t, "alpine:3.18", from)

	format, _ := cmd.Flags().GetString("image-format")
	assert.Equal(t, "OCI", format)

	user, _ := cmd.Flags().GetString("user")
	assert.Equal(t, "app", user)

	verbosity, _ := cmd.Flags().GetString("verbosity")
	assert.Equal(t, "debug", verbosity)

	insecure, _ := cmd.Flags().GetBool("allow-insecure-registries")
	assert.True(t, insecure)
}

func TestBuildCmd_DefaultValues(t *testing.T) {
	cmd := newBuildCmd()

	bf, _ := cmd.Flags().GetString("build-file")
	assert.Equal(t, "jib.yaml", bf)

	ctx, _ := cmd.Flags().GetString("context")
	assert.Equal(t, ".", ctx)

	verbosity, _ := cmd.Flags().GetString("verbosity")
	assert.Equal(t, "lifecycle", verbosity)

	insecure, _ := cmd.Flags().GetBool("allow-insecure-registries")
	assert.False(t, insecure)
}

func TestBuildCmd_AdditionalTags(t *testing.T) {
	cmd := newBuildCmd()
	err := cmd.ParseFlags([]string{
		"--target", "gcr.io/proj/app:v1",
		"--additional-tags", "latest,v1.0",
	})
	require.NoError(t, err)

	tags, _ := cmd.Flags().GetStringSlice("additional-tags")
	assert.Equal(t, []string{"latest", "v1.0"}, tags)
}

func TestBuildCmd_Parameters(t *testing.T) {
	cmd := newBuildCmd()
	err := cmd.ParseFlags([]string{
		"--target", "gcr.io/proj/app:v1",
		"--parameter", "key1=val1",
		"--parameter", "key2=val2",
	})
	require.NoError(t, err)

	params, _ := cmd.Flags().GetStringToString("parameter")
	assert.Equal(t, "val1", params["key1"])
	assert.Equal(t, "val2", params["key2"])
}

func TestBuildCmd_FromCredentialFlags(t *testing.T) {
	cmd := newBuildCmd()
	err := cmd.ParseFlags([]string{
		"--target", "gcr.io/proj/app:v1",
		"--from-username", "from-user",
		"--from-password", "from-pass",
		"--from-credential-helper", "gcr",
	})
	require.NoError(t, err)

	fromUser, _ := cmd.Flags().GetString("from-username")
	assert.Equal(t, "from-user", fromUser)

	fromPass, _ := cmd.Flags().GetString("from-password")
	assert.Equal(t, "from-pass", fromPass)

	fromHelper, _ := cmd.Flags().GetString("from-credential-helper")
	assert.Equal(t, "gcr", fromHelper)
}

func TestBuildCmd_ToCredentialFlags(t *testing.T) {
	cmd := newBuildCmd()
	err := cmd.ParseFlags([]string{
		"--target", "gcr.io/proj/app:v1",
		"--to-username", "to-user",
		"--to-password", "to-pass",
		"--to-credential-helper", "ecr",
	})
	require.NoError(t, err)

	toUser, _ := cmd.Flags().GetString("to-username")
	assert.Equal(t, "to-user", toUser)

	toPass, _ := cmd.Flags().GetString("to-password")
	assert.Equal(t, "to-pass", toPass)

	toHelper, _ := cmd.Flags().GetString("to-credential-helper")
	assert.Equal(t, "ecr", toHelper)
}

func TestBuildCmd_CredentialPrecedence(t *testing.T) {
	cmd := newBuildCmd()
	err := cmd.ParseFlags([]string{
		"--target", "gcr.io/proj/app:v1",
		"--username", "generic-user",
		"--password", "generic-pass",
		"--to-username", "to-user",
		"--to-password", "to-pass",
		"--credential-helper", "generic-helper",
		"--from-credential-helper", "from-helper",
	})
	require.NoError(t, err)

	// Generic credentials
	user, _ := cmd.Flags().GetString("username")
	assert.Equal(t, "generic-user", user)

	// To-specific should override generic
	toUser, _ := cmd.Flags().GetString("to-username")
	assert.Equal(t, "to-user", toUser)

	// From-specific helper should override generic
	fromHelper, _ := cmd.Flags().GetString("from-credential-helper")
	assert.Equal(t, "from-helper", fromHelper)
}

func TestBuildCmd_SendCredentialsOverHTTP(t *testing.T) {
	cmd := newBuildCmd()
	err := cmd.ParseFlags([]string{
		"--target", "gcr.io/proj/app:v1",
		"--send-credentials-over-http",
	})
	require.NoError(t, err)

	send, _ := cmd.Flags().GetBool("send-credentials-over-http")
	assert.True(t, send)
}

func TestBuildCmd_DefaultCredentialValues(t *testing.T) {
	cmd := newBuildCmd()

	user, _ := cmd.Flags().GetString("username")
	assert.Empty(t, user)

	pass, _ := cmd.Flags().GetString("password")
	assert.Empty(t, pass)

	helper, _ := cmd.Flags().GetString("credential-helper")
	assert.Empty(t, helper)

	send, _ := cmd.Flags().GetBool("send-credentials-over-http")
	assert.False(t, send)
}
