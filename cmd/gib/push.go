package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jbadeau/gib"
)

type pushFlags struct {
	credentialHelper        string
	username                string
	password                string
	toCredentialHelper      string
	toUsername              string
	toPassword              string
	allowInsecureRegistries bool
	sendCredentialsOverHTTP bool
}

func newPushCmd() *cobra.Command {
	f := &pushFlags{}
	cmd := &cobra.Command{
		Use:   "push <tarball> <reference>",
		Short: "Push an image tarball to a registry, byte for byte",
		Long: `Push the image an image tarball holds to a registry, tagged <reference>.

Every blob and manifest is pushed with the bytes the tarball holds, so the
digest the registry reports is the one the tarball was written with. An OCI
image layout (index.json) naming one image pushes that image; one whose
index.json is itself an index of several architectures, as apko writes,
pushes that index. A docker-save archive (manifest.json only) pushes its
config and layers under the Docker schema 2 manifest describing them.

The reference names a tag, never a digest. The pushed digest is printed.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPush(cmd, f, args[0], args[1])
		},
	}
	cmd.Flags().StringVar(&f.credentialHelper, "credential-helper", "", "credential helper suffix")
	cmd.Flags().StringVar(&f.username, "username", "", "registry username")
	cmd.Flags().StringVar(&f.password, "password", "", "registry password")
	cmd.Flags().StringVar(&f.toCredentialHelper, "to-credential-helper", "", "target credential helper")
	cmd.Flags().StringVar(&f.toUsername, "to-username", "", "target registry username")
	cmd.Flags().StringVar(&f.toPassword, "to-password", "", "target registry password")
	cmd.Flags().BoolVar(&f.allowInsecureRegistries, "allow-insecure-registries", false, "allow HTTP registries")
	cmd.Flags().BoolVar(&f.sendCredentialsOverHTTP, "send-credentials-over-http", false, "allow sending credentials over HTTP")
	return cmd
}

func runPush(cmd *cobra.Command, f *pushFlags, tarPath, ref string) error {
	opts := targetOptions(f.username, f.password, f.credentialHelper, f.toUsername, f.toPassword, f.toCredentialHelper)
	if f.allowInsecureRegistries {
		opts = append(opts, gib.WithAllowInsecureRegistries(true))
	}
	if f.sendCredentialsOverHTTP {
		opts = append(opts, gib.WithSendCredentialsOverHTTP(true))
	}
	result, err := gib.ToRegistry(ref, opts...).Push(cmd.Context(), tarPath)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stderr, "Pushed %s\n", result.TargetImage)
	_, err = fmt.Fprintln(cmd.OutOrStdout(), result.Digest.String())
	return err
}

// targetOptions are the credentials for the registry an image is pushed
// to: the --to-* flags, falling back to the general ones.
func targetOptions(username, password, helper, toUsername, toPassword, toHelper string) []gib.ContainerizerOption {
	var opts []gib.ContainerizerOption
	if toUsername == "" {
		toUsername = username
	}
	if toPassword == "" {
		toPassword = password
	}
	if toUsername != "" && toPassword != "" {
		opts = append(opts, gib.WithCredentials(toUsername, toPassword))
	}
	if toHelper == "" {
		toHelper = helper
	}
	if toHelper != "" {
		opts = append(opts, gib.WithCredentialHelper(toHelper))
	}
	return opts
}
