// Command kubectl-list_webhooks is a kubectl plugin that lists admission
// webhooks (mutating and validating) and the resources and verbs they
// intercept. Installed on PATH as `kubectl-list_webhooks`, it is invoked as
// `kubectl list-webhooks`.
package main

import (
	"os"

	// Register client-go auth plugins so cloud-provider kubeconfigs
	// (GKE, EKS, AKS, OIDC, ...) authenticate correctly.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/ronaknnathani/kubectl-list-webhooks/pkg/cli"
)

// Populated at build time via -ldflags.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	os.Exit(cli.Execute(cli.BuildInfo{Version: version, Commit: commit, Date: date}))
}
