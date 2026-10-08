package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/jbadeau/gib"
	"github.com/jbadeau/gib/buildfile"
)

// prompt is the value an interactive password option takes when it is
// given without one: the password is then read from the console.
const prompt = "\x00prompt"

// buildFlags are Jib's `build` options: its own and CommonCliOptions.
type buildFlags struct {
	credentialFlags
	target, name                     string
	buildFile, context               string
	parameters                       map[string]string
	additionalTags                   []string
	baseImageCache, projectCache     string
	allowInsecure, sendCredsOverHTTP bool
	fromCredentialHelper             string
	fromUsername, fromPassword       string
	verbosity, console, httpTrace    string
	stacktrace, serialize, version   bool
	imageMetadataOut                 string
}

var passwordPrompts = map[string]string{
	"password":      "password for communicating with both target and base image registries",
	"to-password":   "password for communicating with target image registry",
	"from-password": "password for communicating with base image registry",
}

func newBuildCmd(args []string, stdin io.Reader) *cobra.Command {
	f := &buildFlags{parameters: map[string]string{}, context: ".", verbosity: "lifecycle", console: "auto", httpTrace: "off"}
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build a container",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, positional []string) error {
			return runBuild(cmd, f, args, positional, stdin)
		},
	}
	fs := cmd.Flags()
	str := func(p *string, name, short, label, usage string) *pflag.Flag {
		fs.VarP(&once{name: name, label: label, value: p}, name, short, usage)
		fl := fs.Lookup(name)
		fl.Annotations = map[string][]string{"label": {label}}
		return fl
	}
	enum := func(p *string, name, label, usage string, choices ...string) *pflag.Flag {
		fs.Var(&choice{once: once{name: name, label: label, value: p}, choices: choices}, name, usage)
		fl := fs.Lookup(name)
		fl.Annotations = map[string][]string{"label": {label}}
		return fl
	}

	str(&f.target, "target", "t", "<target-image>", "The destination image reference or jib style url,\nexamples:\n gcr.io/project/image,\n registry://image-ref,\n docker://image,\n tar://path")
	str(&f.buildFile, "build-file", "b", "<build-file>", "The path to the build file (ex: path/to/other-jib.yaml)")
	str(&f.context, "context", "c", "<project-root>", "The context root directory of the build (ex: path/to/my/build/things)")
	fs.VarP(&keyValues{name: "parameter", label: "<name>=<value>", m: f.parameters}, "parameter", "p",
		"templating parameter to inject into build file, replace ${<name>} with <value> (repeatable)")
	str(&f.name, "name", "", "<image-reference>", "The image reference to inject into the tar configuration (required when using --target tar://...)")
	fs.Var(&list{values: &f.additionalTags}, "additional-tags", "Additional tags for target image")
	str(&f.baseImageCache, "base-image-cache", "", "<cache-directory>", "A path to a base image cache")
	str(&f.projectCache, "project-cache", "", "<cache-directory>", "A path to the project cache")
	fs.BoolVar(&f.allowInsecure, "allow-insecure-registries", false, "Allow jib to communicate with registries over http (insecure)")
	fs.BoolVar(&f.sendCredsOverHTTP, "send-credentials-over-http", false, "Allow jib to send credentials over http (very insecure)")
	str(&f.credentialHelper, "credential-helper", "", "<credential-helper>",
		"credential helper for communicating with both target and base image registries, either a path to the helper, or a suffix for an executable named `docker-credential-<suffix>`")
	str(&f.username, "username", "", "<username>", "username for communicating with both target and base image registries")
	str(&f.password, "password", "", "<password>", passwordPrompts["password"]).NoOptDefVal = prompt
	str(&f.toCredentialHelper, "to-credential-helper", "", "<credential-helper>",
		"credential helper for communicating with target registry, either a path to the helper, or a suffix for an executable named `docker-credential-<suffix>`")
	str(&f.toUsername, "to-username", "", "<username>", "username for communicating with target image registry")
	str(&f.toPassword, "to-password", "", "<password>", passwordPrompts["to-password"]).NoOptDefVal = prompt
	str(&f.fromCredentialHelper, "from-credential-helper", "", "<credential-helper>",
		"credential helper for communicating with base image registry, either a path to the helper, or a suffix for an executable named `docker-credential-<suffix>`")
	str(&f.fromUsername, "from-username", "", "<username>", "username for communicating with base image registry")
	str(&f.fromPassword, "from-password", "", "<password>", passwordPrompts["from-password"]).NoOptDefVal = prompt
	enum(&f.verbosity, "verbosity", "<level>", "set logging verbosity, candidates: quiet, error, warn, lifecycle, info, debug, default: lifecycle", verbosities...)
	enum(&f.console, "console", "<type>", "set console output type, candidates: auto, rich, plain, default: auto", "auto", "rich", "plain")
	fs.BoolVar(&f.stacktrace, "stacktrace", false, "")
	enum(&f.httpTrace, "http-trace", "<httpTrace>", "set http logging level, candidates: off, config, all, default: off", "off", "config", "all").NoOptDefVal = "config"
	fs.BoolVar(&f.serialize, "serialize", false, "")
	str(&f.imageMetadataOut, "image-metadata-out", "", "<path-to-json>", "path to the json file that should contain image metadata (for example, digest, id and tags) after build is complete")
	fs.BoolVarP(&f.version, "version", "V", false, "Print version information and exit.")
	for _, h := range []string{"stacktrace", "http-trace", "serialize"} {
		_ = fs.MarkHidden(h)
	}
	return cmd
}

func runBuild(cmd *cobra.Command, f *buildFlags, args, positional []string, stdin io.Reader) error {
	if f.version {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), gib.Version())
		return err
	}
	if err := f.prompt(cmd, stdin); err != nil {
		return err
	}
	if err := f.validate(cmd, args, positional); err != nil {
		return err
	}

	con := newConsole(f.verbosity, f.console, f.httpTrace != "off", cmd.OutOrStdout(), cmd.ErrOrStderr())
	fail := func(err error) error {
		if f.stacktrace {
			for e := err; e != nil; e = errors.Unwrap(e) {
				con.errorf("%T: %s", e, e)
			}
		}
		con.errorf("%s", message(err))
		return errFailed
	}

	mirrors, err := registryMirrors()
	if err != nil {
		return fail(err)
	}
	file := f.buildFile
	if file == "" {
		file = resolve(f.context, "jib.yaml")
	}
	fi, err := os.Stat(file)
	if err != nil || !readable(file) {
		return fail(fmt.Errorf("The Build File YAML either does not exist or cannot be opened for reading: %s", file)) //nolint:staticcheck // Jib's message
	}
	if !fi.Mode().IsRegular() {
		return fail(fmt.Errorf("Build File YAML path is not a file: %s", file)) //nolint:staticcheck // Jib's message
	}
	if fi, err := os.Stat(f.context); err != nil || !fi.IsDir() {
		return fail(fmt.Errorf("contextRoot must be a directory, but %s is not.", f.context)) //nolint:staticcheck // Jib's message
	}

	target := f.containerizer(con, mirrors, cmd.Flags().Changed)
	if err := target.Validate(); err != nil {
		return fail(err)
	}
	spec, err := buildfile.Parse(file, f.parameters)
	if err != nil {
		return fail(err)
	}
	builder, err := buildfile.Convert(spec, f.context, nil)
	if err != nil {
		return fail(err)
	}
	builder.ConfigureBaseImage(f.baseOptions(cmd.Flags().Changed)...)
	layers := 0
	if spec.Layers != nil {
		layers = len(spec.Layers.Entries)
	}
	builder.OnLog(con.log).OnProgress(con.progress(layers))

	result, err := builder.Containerize(cmd.Context(), target)
	con.done()
	if err != nil {
		return fail(err)
	}
	if f.imageMetadataOut != "" {
		data, err := result.Metadata()
		if err != nil {
			return fail(err)
		}
		if err := os.WriteFile(f.imageMetadataOut, data, 0o644); err != nil {
			return fail(err)
		}
	}
	return nil
}

// message is the message of the error that ended a build: the one Jib
// would print, when gib knows it.
func message(err error) string {
	var (
		insecure  *gib.InsecureRegistryError
		notSent   *gib.CredentialsNotSentError
		notFound  *gib.HelperNotFoundError
		missing   *gib.HelperMissingError
		reference *gib.InvalidReferenceError
	)
	switch {
	case errors.As(err, &insecure):
		return insecure.Error()
	case errors.As(err, &notSent):
		return notSent.Error()
	case errors.As(err, &notFound):
		return notFound.Error()
	case errors.As(err, &missing):
		return missing.Error()
	case errors.As(err, &reference):
		return reference.Error()
	}
	return err.Error()
}

func readable(file string) bool {
	r, err := os.Open(file)
	if err != nil {
		return false
	}
	_ = r.Close()
	return true
}

// resolve is name in dir, as Java's Path.resolve spells it.
func resolve(dir, name string) string {
	if dir == "" {
		return name
	}
	if d := strings.TrimRight(dir, "/"); d != "" {
		return d + "/" + name
	}
	return "/" + name
}

// prompt reads each password given without a value from the console, as
// picocli reads an interactive option's.
func (f *buildFlags) prompt(cmd *cobra.Command, stdin io.Reader) error {
	var lines *bufio.Reader
	for _, p := range []struct {
		name  string
		value *string
	}{{"password", &f.password}, {"to-password", &f.toPassword}, {"from-password", &f.fromPassword}} {
		if *p.value != prompt {
			continue
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Enter value for --%s (%s): ", p.name, passwordPrompts[p.name])
		if in, ok := stdin.(*os.File); ok && term.IsTerminal(in.Fd()) {
			b, err := term.ReadPassword(in.Fd())
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			*p.value = string(b)
			continue
		}
		if lines == nil {
			lines = bufio.NewReader(stdin)
		}
		line, err := lines.ReadString('\n')
		if err != nil && line == "" {
			*p.value = ""
			continue
		}
		*p.value = strings.TrimRight(line, "\r\n")
	}
	return nil
}

const credentialGroups = "--credential-helper=<credential-helper> and [--username=<username> --password[=<password>]] and " +
	"[[--to-credential-helper=<credential-helper> | [--to-username=<username> --to-password[=<password>]]] " +
	"[--from-credential-helper=<credential-helper> | [--from-username=<username> --from-password[=<password>]]]]"

// validate refuses what picocli refuses of a build's command line: a
// missing target, credential options that exclude each other or lack
// their pair, arguments that are no option's, and a tarball target
// without --name.
func (f *buildFlags) validate(cmd *cobra.Command, args, positional []string) error {
	set := cmd.Flags().Changed
	if !set("target") {
		return usagef(cmd, "Missing required option: '--target=<target-image>'")
	}
	single := set("username") || set("password")
	separate := false
	for _, n := range []string{"to-credential-helper", "to-username", "to-password", "from-credential-helper", "from-username", "from-password"} {
		separate = separate || set(n)
	}
	n := 0
	for _, b := range []bool{set("credential-helper"), single, separate} {
		if b {
			n++
		}
	}
	if n > 1 {
		return usagef(cmd, "Error: %s are mutually exclusive (specify only one)", credentialGroups)
	}
	for _, prefix := range []string{"", "to-", "from-"} {
		user, pass := set(prefix+"username"), set(prefix+"password")
		if prefix != "" && set(prefix+"credential-helper") && (user || pass) {
			return usagef(cmd, "Error: --%scredential-helper=<credential-helper> and [--%susername=<username> --%spassword[=<password>]] are mutually exclusive (specify only one)",
				prefix, prefix, prefix)
		}
		if pass && !user {
			return usagef(cmd, "Error: Missing required argument(s): --%susername=<username>", prefix)
		}
		if user && !pass {
			return usagef(cmd, "Error: Missing required argument(s): --%spassword", prefix)
		}
	}
	if len(positional) > 0 {
		i := unmatched(cmd.Flags(), args)
		if len(positional) == 1 {
			return usagef(cmd, "Unmatched argument at index %d: '%s'", i, positional[0])
		}
		return usagef(cmd, "Unmatched arguments from index %d: '%s'", i, strings.Join(positional, "', '"))
	}
	if strings.HasPrefix(f.target, "tar://") && !set("name") {
		return usagef(cmd, "Missing option: --name must be specified when using --target=tar://....")
	}
	return nil
}

// unmatched is the index in args of the first argument that is neither
// the subcommand, an option nor an option's value.
func unmatched(fs *pflag.FlagSet, args []string) int {
	sub := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return i + 1
		case strings.HasPrefix(a, "--"):
			name, _, value := strings.Cut(a[2:], "=")
			fl := fs.Lookup(name)
			if fl == nil || value || fl.Value.Type() == "bool" {
				continue
			}
			if fl.NoOptDefVal == "" || (i+1 < len(args) && !strings.HasPrefix(args[i+1], "-")) {
				i++
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			fl := fs.ShorthandLookup(a[1:2])
			if fl != nil && len(a) == 2 && fl.Value.Type() != "bool" {
				i++
			}
		case !sub:
			sub = true
		default:
			return i
		}
	}
	return len(args)
}

// containerizer is the build's target, as Jib's Containerizers makes it.
func (f *buildFlags) containerizer(con *console, mirrors []mirror, set func(string) bool) *gib.Containerizer {
	opts := []gib.ContainerizerOption{
		gib.WithLogHandler(con.log),
		gib.WithAllowInsecureRegistries(f.allowInsecure),
		gib.WithSendCredentialsOverHTTP(f.sendCredsOverHTTP),
		gib.WithSerialize(f.serialize),
	}
	for _, t := range f.additionalTags {
		opts = append(opts, gib.WithAdditionalTag(t))
	}
	for _, m := range mirrors {
		opts = append(opts, gib.WithRegistryMirrors(m.Registry, m.Mirrors...))
	}
	switch f.httpTrace {
	case "config":
		opts = append(opts, gib.WithHTTPTrace(gib.TraceConfig, con.err))
	case "all":
		opts = append(opts, gib.WithHTTPTrace(gib.TraceAll, con.err))
	}
	switch {
	case strings.HasPrefix(f.target, "docker://"):
		return gib.ToDocker(strings.TrimPrefix(f.target, "docker://"), opts...)
	case strings.HasPrefix(f.target, "tar://"):
		return gib.ToTar(strings.TrimPrefix(f.target, "tar://"), append(opts, gib.WithTarImageName(f.name))...)
	}
	opts = append(opts, f.targetCredentials(set)...)
	return gib.ToRegistry(f.target, opts...)
}

// baseOptions are the base image registry's credentials, as Jib's
// Credentials.getFromCredentialRetrievers takes them.
func (f *buildFlags) baseOptions(set func(string) bool) []gib.ImageSourceOption {
	var opts []gib.ImageSourceOption
	switch {
	case set("username"):
		opts = append(opts, gib.WithSourceCredential(gib.Credential{Username: f.username, Password: f.password}, "--username/--password"))
	case set("from-username"):
		opts = append(opts, gib.WithSourceCredential(gib.Credential{Username: f.fromUsername, Password: f.fromPassword}, "--from-username/--from-password"))
	}
	if h := f.credentialHelper + f.fromCredentialHelper; h != "" {
		opts = append(opts, gib.WithSourceCredentialHelper(h))
	}
	return opts
}
