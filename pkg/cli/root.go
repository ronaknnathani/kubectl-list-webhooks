// Package cli implements the `kubectl list-webhooks` command.
package cli

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/ronaknnathani/kubectl-list-webhooks/pkg/output"
	"github.com/ronaknnathani/kubectl-list-webhooks/pkg/webhooks"
)

const examples = `  # List every webhook, one row per resource and verb
  kubectl list-webhooks

  # Validating webhooks that intercept pod creation
  kubectl list-webhooks --type validating --resource pods --verb CREATE

  # Count webhooks and rules per type, broken down by resource
  kubectl list-webhooks --summary --by-resource`

// BuildInfo carries version metadata injected at build time.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// clientFactory builds a Kubernetes client from a kubeconfig path, where ""
// means the standard loading rules.
type clientFactory func(kubeconfig string) (kubernetes.Interface, error)

type options struct {
	kubeconfig  string
	webhookType string
	verb        string
	resource    string
	summary     bool
	byResource  bool
	byVerb      bool

	newClient clientFactory
}

// Execute runs the command and returns the process exit code.
func Execute(build BuildInfo) int {
	if err := newCommand(build, newClient).Execute(); err != nil {
		// Prefix every line, since a joined error spans several.
		fmt.Fprintln(os.Stderr, "error: "+strings.ReplaceAll(err.Error(), "\n", "\nerror: "))
		return 1
	}
	return 0
}

// newCommand builds the root cobra command, using newClient to reach the cluster.
func newCommand(build BuildInfo, newClient clientFactory) *cobra.Command {
	o := &options{newClient: newClient}
	cmd := &cobra.Command{
		Use:     "list-webhooks",
		Short:   "List admission webhooks and the resources and verbs they intercept",
		Example: examples,
		// Display the kubectl plugin form in help, usage, and --version output.
		Annotations:   map[string]string{cobra.CommandDisplayNameAnnotation: "kubectl list-webhooks"},
		Version:       fmt.Sprintf("%s (commit: %s, date: %s)", build.Version, build.Commit, build.Date),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return o.run(cmd)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&o.webhookType, "type", "", "Only list webhooks of this type: validating or mutating (default both)")
	flags.StringVar(&o.verb, "verb", "", "Only list webhooks for this operation: CREATE, UPDATE, DELETE, or CONNECT")
	flags.StringVar(&o.resource, "resource", "", "Only list webhooks for this resource, e.g. pods, pods/exec, or deployments.apps")
	flags.BoolVar(&o.summary, "summary", false, "Print webhook and rule counts per type instead of rows")
	flags.BoolVar(&o.byResource, "by-resource", false, "With --summary, break the counts down by resource")
	flags.BoolVar(&o.byVerb, "by-verb", false, "With --summary, break the counts down by verb")
	flags.StringVar(&o.kubeconfig, "kubeconfig", "", "Path to the kubeconfig file (default KUBECONFIG, then ~/.kube/config)")
	return cmd
}

func (o *options) run(cmd *cobra.Command) error {
	types, err := o.validate()
	if err != nil {
		return err
	}
	client, err := o.newClient(o.kubeconfig)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rows, err := webhooks.List(ctx, client, types)
	if err != nil {
		return err
	}
	rows = webhooks.Filter(rows, o.verb, o.resource)

	// Color only when stdout is a terminal.
	f, isFile := cmd.OutOrStdout().(*os.File)
	color := isFile && term.IsTerminal(int(f.Fd()))
	if !o.summary {
		if len(rows) == 0 {
			return printNoWebhooks(cmd)
		}
		return output.Listing(cmd.OutOrStdout(), rows, color)
	}
	// A plain summary always has a row per type; only a breakdown can be empty.
	counts := webhooks.Summarize(rows, types, o.byResource, o.byVerb)
	if len(counts) == 0 {
		return printNoWebhooks(cmd)
	}
	return output.Summary(cmd.OutOrStdout(), counts, o.byResource, o.byVerb, color)
}

// validate checks every flag, reporting all invalid ones together, normalizes
// --verb to upper case, and returns the webhook types to list.
func (o *options) validate() ([]webhooks.Type, error) {
	var errs []error
	types := webhooks.Types
	if o.webhookType != "" {
		t := webhooks.Type(strings.ToLower(o.webhookType))
		if !slices.Contains(webhooks.Types, t) {
			errs = append(errs, fmt.Errorf("invalid --type %q: must be validating or mutating", o.webhookType))
		}
		types = []webhooks.Type{t}
	}
	if o.verb != "" {
		verb := strings.ToUpper(o.verb)
		if !slices.Contains(webhooks.Verbs, verb) {
			errs = append(errs, fmt.Errorf("invalid --verb %q: must be CREATE, UPDATE, DELETE, or CONNECT", o.verb))
		}
		o.verb = verb
	}
	if o.byResource && !o.summary {
		errs = append(errs, errors.New("--by-resource requires --summary"))
	}
	if o.byVerb && !o.summary {
		errs = append(errs, errors.New("--by-verb requires --summary"))
	}
	return types, errors.Join(errs...)
}

// printNoWebhooks reports an empty result on stderr, like kubectl's
// "No resources found".
func printNoWebhooks(cmd *cobra.Command) error {
	if _, err := fmt.Fprintln(cmd.ErrOrStderr(), "No webhooks found."); err != nil {
		return fmt.Errorf("writing empty-result message: %w", err)
	}
	return nil
}

// newClient builds a Kubernetes client from the kubeconfig at path, or from the
// standard loading rules (KUBECONFIG, then ~/.kube/config) when path is "",
// using the current context.
func newClient(path string) (kubernetes.Interface, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = path
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("building Kubernetes client: %w", err)
	}
	return client, nil
}
