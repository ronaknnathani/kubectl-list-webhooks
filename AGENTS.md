# kubectl-list-webhooks

Go-based kubectl plugin that lists the admission webhooks registered in a
Kubernetes cluster. It reads `admissionregistration.k8s.io/v1`
MutatingWebhookConfigurations and ValidatingWebhookConfigurations and prints one
row per (webhook, resource, verb) to stdout. It is read-only: it makes at most
two LIST calls (one per configuration kind), does all filtering in memory, and
makes no GET or discovery calls.

## Build, test, and lint

Use the Makefile targets; they pin and bootstrap tooling where needed.

```bash
make build          # go mod tidy, tests, lint, then build
make test           # unit tests
make lint           # pinned golangci-lint v2.12.2
make ci             # local equivalent of GitHub Actions checks
make test-coverage  # coverage report
```

Run `make ci` before pushing meaningful code or workflow changes. For docs-only
changes, at least run `git diff --check`; run more if docs touch commands or
install instructions.

## Development conventions

- Follow standard Go conventions and keep changes focused and surgical.
- Add or update tests for behavior changes; a bug fix needs a test that fails
  without the fix.
- Prefer table-driven tests and the existing fixtures; construct fake clusters
  with the fake clientset (`k8s.io/client-go/kubernetes/fake`). Do not require a
  live cluster for unit tests.
- Preserve the tool's **read-only** behavior. It only lists
  MutatingWebhookConfigurations and ValidatingWebhookConfigurations; it must
  never create, update, or delete objects.
- Keep stdout clean for data; errors and diagnostics (including
  `No webhooks found.`) go to stderr.
- Emit ANSI color only when stdout is a terminal.
- Do not commit generated artifacts such as `kubectl-list_webhooks`,
  `coverage.out`, `dist/`, `bin/`, or `.tools/`.

## kubectl plugin conventions

- Help and usage text show the kubectl form (`kubectl list-webhooks`), not just
  the binary name.
- Of the common kubectl flags, support only `-h`/`--help` and `--kubeconfig`
  (plus `--version`); there is no `--context` or `-n`/`--namespace`. Without
  `--kubeconfig`, the `KUBECONFIG` environment variable and `~/.kube/config` are
  used via the standard loading rules.
- Import client-go auth plugins with `_ "k8s.io/client-go/plugin/pkg/client/auth"`
  so cloud-provider kubeconfigs (GKE, EKS, AKS, OIDC) authenticate.
- The binary is named `kubectl-list_webhooks` (underscore) so kubectl exposes it
  as `kubectl list-webhooks`.
- Errors print `error: ...` to stderr and exit with code 1.

## Git, commits, and PRs

- Keep PRs focused on one logical change.
- Use descriptive commit messages, e.g. `Add --verb filter` or
  `Group summary rows by resource`.
- Include an appropriate co-author trailer when committing from an AI-assisted
  session, using the identity of the agent that made the change:

  ```text
  Co-authored-by: <Agent Name> <agent-email@example.com>
  ```

- Before changing GitHub repo metadata or other user-facing descriptions, show
  the exact proposed text first.
- Prefer `gh` for GitHub operations.
- After pushing, check GitHub Actions and report whether CI passed.
