package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jbadeau/gib"
)

// credentialFlags are the target registry's credential options, which
// build and push share.
type credentialFlags struct {
	credentialHelper       string
	username, password     string
	toCredentialHelper     string
	toUsername, toPassword string
}

// targetCredentials are the target registry's credentials, as Jib's
// Credentials.getToCredentialRetrievers takes them.
func (c credentialFlags) targetCredentials(set func(string) bool) []gib.ContainerizerOption {
	var opts []gib.ContainerizerOption
	switch {
	case set("username"):
		opts = append(opts, gib.WithCredential(gib.Credential{Username: c.username, Password: c.password}, "--username/--password"))
	case set("to-username"):
		opts = append(opts, gib.WithCredential(gib.Credential{Username: c.toUsername, Password: c.toPassword}, "--to-username/--to-password"))
	}
	if h := c.credentialHelper + c.toCredentialHelper; h != "" {
		opts = append(opts, gib.WithCredentialHelper(h))
	}
	return opts
}

type pushFlags struct {
	credentialFlags
	allowInsecureRegistries bool
	sendCredentialsOverHTTP bool
}

func newPushCmd() *cobra.Command {
	f := &pushFlags{}
	cmd := &cobra.Command{
		Use:   "push <tarball>... <reference>",
		Short: "Push image tarballs to a registry, byte for byte",
		Long: `Push the image an image tarball holds to a registry, tagged <reference>.

Every blob and manifest is pushed with the bytes the tarball holds, so the
digest the registry reports is the one the tarball was written with. An OCI
image layout (index.json) naming one image pushes that image; one whose
index.json is itself an index of several architectures, as apko writes,
pushes that index. A docker-save archive (manifest.json only) pushes its
config and layers under the Docker schema 2 manifest describing them.

Several tarballs, each one image for one platform as a build for that
platform writes it, are pushed as one image index naming each image by
the platform its config states, in the order given: a Docker manifest list
of images in the Docker format, an OCI index of OCI images. Two images for
one platform, or images of both formats, are refused.

The reference names a tag, never a digest. The pushed digest is printed.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPush(cmd, f, args[:len(args)-1], args[len(args)-1])
		},
	}
	cmd.Flags().StringVar(&f.credentialHelper, "credential-helper", "", "credential helper, a path to one or the suffix of docker-credential-<suffix>")
	cmd.Flags().StringVar(&f.username, "username", "", "registry username")
	cmd.Flags().StringVar(&f.password, "password", "", "registry password")
	cmd.Flags().StringVar(&f.toCredentialHelper, "to-credential-helper", "", "target credential helper")
	cmd.Flags().StringVar(&f.toUsername, "to-username", "", "target registry username")
	cmd.Flags().StringVar(&f.toPassword, "to-password", "", "target registry password")
	cmd.Flags().BoolVar(&f.allowInsecureRegistries, "allow-insecure-registries", false, "allow registries that cannot be verified, then plain HTTP")
	cmd.Flags().BoolVar(&f.sendCredentialsOverHTTP, "send-credentials-over-http", false, "allow sending credentials over HTTP")
	return cmd
}

func runPush(cmd *cobra.Command, f *pushFlags, tarPaths []string, ref string) error {
	opts := f.targetCredentials(cmd.Flags().Changed)
	opts = append(opts,
		gib.WithAllowInsecureRegistries(f.allowInsecureRegistries),
		gib.WithSendCredentialsOverHTTP(f.sendCredentialsOverHTTP))
	c := gib.ToRegistry(ref, opts...)
	var result *gib.Container
	var err error
	if len(tarPaths) == 1 {
		result, err = c.Push(cmd.Context(), tarPaths[0])
	} else {
		result, err = c.PushIndex(cmd.Context(), tarPaths)
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stderr, "Pushed %s\n", result.TargetImage)
	_, err = fmt.Fprintln(cmd.OutOrStdout(), result.Digest.String())
	return err
}
