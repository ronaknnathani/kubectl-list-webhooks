// Package cli implements the `kubectl list-webhooks` command.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// BuildInfo carries version metadata injected at build time.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// Execute runs the command and returns the process exit code.
func Execute(build BuildInfo) int {
	if err := newCommand(build).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

// newCommand builds the root cobra command.
func newCommand(build BuildInfo) *cobra.Command {
	return &cobra.Command{
		Use:   "list-webhooks",
		Short: "List admission webhooks and the resources and verbs they intercept",
		// Display the kubectl plugin form in help, usage, and --version output.
		Annotations:   map[string]string{cobra.CommandDisplayNameAnnotation: "kubectl list-webhooks"},
		Version:       fmt.Sprintf("%s (commit: %s, date: %s)", build.Version, build.Commit, build.Date),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
}
