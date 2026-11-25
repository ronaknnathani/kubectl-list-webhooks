# kubectl list-webhooks

A `kubectl` plugin that lists the admission webhooks in a cluster. It reads
MutatingWebhookConfigurations and ValidatingWebhookConfigurations
(`admissionregistration.k8s.io/v1`) and prints one row per (webhook, resource,
verb). It is read-only.

```text
$ kubectl list-webhooks
TYPE        CONFIGURATION           WEBHOOK                    RESOURCE                      VERB
mutating    workload-defaults       defaults.example.com       deployments.apps              CREATE
mutating    workload-defaults       defaults.example.com       deployments.apps              UPDATE
mutating    istio-sidecar-injector  sidecar-injector.istio.io  pods                          CREATE
mutating    workload-defaults       defaults.example.com       pods                          CREATE
validating  gatekeeper              validation.gatekeeper.sh   *.*                           CREATE
validating  gatekeeper              validation.gatekeeper.sh   *.*                           UPDATE
validating  cert-manager-webhook    webhook.cert-manager.io    certificates.cert-manager.io  CREATE
validating  cert-manager-webhook    webhook.cert-manager.io    certificates.cert-manager.io  UPDATE
validating  cert-manager-webhook    webhook.cert-manager.io    issuers.cert-manager.io       CREATE
validating  cert-manager-webhook    webhook.cert-manager.io    issuers.cert-manager.io       UPDATE
validating  pod-policy              pods.policy.example.com    pods                          CREATE
validating  pod-policy              pods.policy.example.com    pods                          DELETE
validating  pod-policy              pods.policy.example.com    pods/exec                     CONNECT
```

Each row shows the webhook's `TYPE`, the `CONFIGURATION` it belongs to, the
`WEBHOOK` name, and the `RESOURCE` and `VERB` it intercepts.

## How it works

- Walks every configuration, then each webhook, each rule, and each resource ×
  operation in the rule, emitting one row per combination. Duplicate rows are
  printed once.
- `RESOURCE` is the resource name for the core group and `resource.group`
  otherwise, with one row per API group.
- Wildcards are shown as written, not expanded: `*.*` means any resource in any
  group, and `*/*` and `*/status` keep their rule form. `VERB` is also shown as
  written, including `*`.
- Mutating rows come before validating rows (the order admission runs them),
  then rows are sorted by resource, configuration, webhook, and verb. Verbs
  follow API order: `*`, CREATE, UPDATE, DELETE, CONNECT.

## Installation

### Install script

```bash
curl -fsSL https://raw.githubusercontent.com/ronaknnathani/kubectl-list-webhooks/main/install.sh | sh
```

By default, the script installs to `/usr/local/bin` when writable, otherwise `~/.local/bin`. Override with `INSTALL_DIR=/path/to/bin`.

### krew

Once the plugin is available in the [krew index](https://krew.sigs.k8s.io):

```bash
kubectl krew install list-webhooks
```

### GitHub releases

Download the latest archive for your platform from the [releases page](https://github.com/ronaknnathani/kubectl-list-webhooks/releases), then place the `kubectl-list_webhooks` binary on your `PATH`.

### Source

```bash
git clone https://github.com/ronaknnathani/kubectl-list-webhooks.git
cd kubectl-list-webhooks
make build
make install
```

Requires Go 1.26+.

The kubectl plugin mechanism turns a `kubectl-list_webhooks` binary on your `PATH`
into the `kubectl list-webhooks` subcommand (the `_` becomes `-`). Verify with:

```bash
kubectl list-webhooks --help
kubectl plugin list | grep list-webhooks
```

## Usage

| Flag | Description |
|------|-------------|
| `--type` | Only list webhooks of this type: `validating` or `mutating` (case-insensitive). Default both. |
| `--verb` | Only list webhooks for this operation: `CREATE`, `UPDATE`, `DELETE`, or `CONNECT` (case-insensitive). Also matches rules whose verb is `*`. |
| `--resource` | Only list webhooks for this resource, e.g. `pods`, `pods/exec`, or `deployments.apps`. Matches covering wildcards (`*`, `*/*`, `*/sub`, `resource/*`). A group in the value must match the rule's group or `*`; without a group, any group matches. |
| `--summary` | Print webhook and rule counts per type instead of rows. |
| `--by-resource` | Break the summary down by resource. Requires `--summary`. |
| `--by-verb` | Break the summary down by verb. Requires `--summary`; combines with `--by-resource`. |
| `--kubeconfig` | Path to the kubeconfig file. |
| `-h, --help` | Show help. |
| `--version` | Show the version. |

Filters combine with AND.

There is no `--context` or `-n/--namespace` flag. Without `--kubeconfig`, the
`KUBECONFIG` environment variable and then `~/.kube/config` are used, with the
current context.

### Examples

Filters match wildcard rules too, so gatekeeper's `*.*` rule shows up for pods:

```text
$ kubectl list-webhooks --type validating --resource pods --verb CREATE
TYPE        CONFIGURATION  WEBHOOK                   RESOURCE  VERB
validating  gatekeeper     validation.gatekeeper.sh  *.*       CREATE
validating  pod-policy     pods.policy.example.com   pods      CREATE
```

`--summary` counts distinct webhooks and matching rows per type, never combined
across types. Without `--type`, both types are shown, even at zero:

```text
$ kubectl list-webhooks --summary
TYPE        WEBHOOKS  RULES
mutating    2         4
validating  3         9
```

With `--by-resource`, wildcard rules count under their own row:

```text
$ kubectl list-webhooks --summary --by-resource
TYPE        RESOURCE                      WEBHOOKS  RULES
mutating    deployments.apps              1         2
mutating    pods                          2         2
validating  *.*                           1         2
validating  certificates.cert-manager.io  1         2
validating  issuers.cert-manager.io       1         2
validating  pods                          1         2
validating  pods/exec                     1         1
```

With `--by-verb`, verbs follow API order:

```text
$ kubectl list-webhooks --summary --by-verb
TYPE        VERB     WEBHOOKS  RULES
mutating    CREATE   2         3
mutating    UPDATE   1         1
validating  CREATE   3         4
validating  UPDATE   2         3
validating  DELETE   1         1
validating  CONNECT  1         1
```

## Output

Color is used only when stdout is a terminal: mutating rows are magenta,
validating rows are cyan, alternating between two shades per resource group (per
verb with `--by-verb`), and the header is bold. Piped output is plain text.

When nothing matches, `No webhooks found.` is printed to stderr and the exit
code is 0.

Errors print `error: ...` to stderr and exit with code 1. Every invalid flag is
reported, not just the first:

```text
error: invalid --type "foo": must be validating or mutating
```

## Permissions and API cost

The plugin needs `list` on `mutatingwebhookconfigurations` and
`validatingwebhookconfigurations` (`admissionregistration.k8s.io`,
cluster-scoped). It performs no writes and makes at most two LIST calls (one
per configuration kind, or one with `--type`), no GET or discovery calls, and
does all filtering in memory.

## Not covered

- ValidatingAdmissionPolicy and MutatingAdmissionPolicy.
- Namespace and object selectors.
- Failure policy and other webhook settings, such as side effects and client
  config.
- JSON or YAML output.

## License

MIT. See [LICENSE](LICENSE).
