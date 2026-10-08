package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"charm.land/fang/v2"
	"github.com/spf13/cobra"

	"github.com/jbadeau/gib"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// usageError is a command line Jib's picocli refuses: its message, the
// usage, and exit code 2.
type usageError struct {
	cmd *cobra.Command
	msg string
}

func (e *usageError) Error() string { return e.msg }

func usagef(cmd *cobra.Command, format string, args ...any) error {
	return &usageError{cmd: cmd, msg: fmt.Sprintf(format, args...)}
}

// errFailed is a build that failed and logged why: exit code 1.
var errFailed = errors.New("build failed")

// run runs gib with args, as Jib's CLI runs: @files expanded first,
// exit code 2 for a command line it refuses and 1 for a build that
// fails.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	args, err := expandArgFiles(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	args = attachOptionalValues(args)

	root := &cobra.Command{
		Use:   "gib",
		Short: "Go Container Builder - daemonless container image builder",
		Long:  "Build container images without Docker, compatible with jib.yaml build files.",
		Args: func(cmd *cobra.Command, a []string) error {
			if len(a) > 0 {
				return usagef(cmd, "Unmatched argument at index 0: '%s'", a[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usagef(cmd, "Missing required subcommand")
		},
	}
	root.Flags().BoolP("version", "V", false, "Print version information and exit.")
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetFlagErrorFunc(flagError)
	root.AddCommand(newBuildCmd(args, stdin), newPushCmd())
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	err = fang.Execute(ctx, root,
		fang.WithVersion(gib.Version()),
		fang.WithNotifySignal(os.Interrupt),
		fang.WithErrorHandler(func(w io.Writer, styles fang.Styles, err error) {
			var u *usageError
			switch {
			case errors.As(err, &u):
				_, _ = fmt.Fprintf(w, "%s\nUsage: %s\nRun '%s --help' for more information on usage.\n",
					u.msg, u.cmd.UseLine(), u.cmd.CommandPath())
			case errors.Is(err, errFailed):
			default:
				fang.DefaultErrorHandler(w, styles, err)
			}
		}),
	)
	var u *usageError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &u):
		return 2
	}
	return 1
}
