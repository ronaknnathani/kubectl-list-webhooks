package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/spf13/pflag"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

var build = BuildInfo{Version: "v9.9.9", Commit: "abc", Date: "d"}

// rule builds a webhook rule for the given groups, resources, and operations.
func rule(groups, resources []string, ops ...admissionregistrationv1.OperationType) admissionregistrationv1.RuleWithOperations {
	return admissionregistrationv1.RuleWithOperations{
		Operations: ops,
		Rule:       admissionregistrationv1.Rule{APIGroups: groups, APIVersions: []string{"v1"}, Resources: resources},
	}
}

// newFakeClient returns a fake clientset holding one mutating and one
// validating webhook configuration.
func newFakeClient() *fake.Clientset {
	return fake.NewClientset(
		&admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "injector"},
			Webhooks: []admissionregistrationv1.MutatingWebhook{{
				Name: "inject.example.com",
				Rules: []admissionregistrationv1.RuleWithOperations{
					rule([]string{""}, []string{"pods"}, admissionregistrationv1.Create, admissionregistrationv1.Update),
					rule([]string{"apps"}, []string{"deployments"}, admissionregistrationv1.Create),
				},
			}},
		},
		&admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "policy"},
			Webhooks: []admissionregistrationv1.ValidatingWebhook{
				{Name: "all.policy.io", Rules: []admissionregistrationv1.RuleWithOperations{
					rule([]string{"*"}, []string{"*"}, admissionregistrationv1.OperationAll),
				}},
				{Name: "pods.policy.io", Rules: []admissionregistrationv1.RuleWithOperations{
					rule([]string{""}, []string{"pods", "pods/exec"}, admissionregistrationv1.Create, admissionregistrationv1.Connect),
				}},
			},
		},
	)
}

// run executes the command against client and returns stdout, stderr, and
// the error text ("" for none).
func run(t *testing.T, client kubernetes.Interface, args ...string) (stdout, stderr, errText string) {
	t.Helper()
	cmd := newCommand(build, func(string) (kubernetes.Interface, error) { return client, nil })
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		errText = err.Error()
	}
	return out.String(), errOut.String(), errText
}

func TestCommand(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantOut    string
		wantStderr string
		wantErr    string
		// wantListed are the resources the command LISTed, sorted; nil when
		// the command must not reach the cluster.
		wantListed []string
	}{
		{
			name:    "version",
			args:    []string{"--version"},
			wantOut: "kubectl list-webhooks version v9.9.9 (commit: abc, date: d)\n",
		},
		{
			name:    "positional args rejected",
			args:    []string{"foo"},
			wantErr: `unknown command "foo" for "kubectl list-webhooks"`,
		},
		{
			name:    "invalid type",
			args:    []string{"--type", "foo"},
			wantErr: `invalid --type "foo": must be validating or mutating`,
		},
		{
			name:    "invalid verb",
			args:    []string{"--verb", "patch"},
			wantErr: `invalid --verb "patch": must be CREATE, UPDATE, DELETE, or CONNECT`,
		},
		{
			name:    "by-resource requires summary",
			args:    []string{"--by-resource"},
			wantErr: "--by-resource requires --summary",
		},
		{
			name:    "by-verb requires summary",
			args:    []string{"--by-verb"},
			wantErr: "--by-verb requires --summary",
		},
		{
			name:    "every invalid flag is reported",
			args:    []string{"--type", "foo", "--by-verb"},
			wantErr: "invalid --type \"foo\": must be validating or mutating\n--by-verb requires --summary",
		},
		{
			name: "default listing",
			wantOut: `TYPE        CONFIGURATION  WEBHOOK             RESOURCE          VERB
mutating    injector       inject.example.com  deployments.apps  CREATE
mutating    injector       inject.example.com  pods              CREATE
mutating    injector       inject.example.com  pods              UPDATE
validating  policy         all.policy.io       *.*               *
validating  policy         pods.policy.io      pods              CREATE
validating  policy         pods.policy.io      pods              CONNECT
validating  policy         pods.policy.io      pods/exec         CREATE
validating  policy         pods.policy.io      pods/exec         CONNECT
`,
			wantListed: []string{"mutatingwebhookconfigurations", "validatingwebhookconfigurations"},
		},
		{
			name: "type lists only that kind and is case-insensitive",
			args: []string{"--type", "Validating"},
			wantOut: `TYPE        CONFIGURATION  WEBHOOK         RESOURCE   VERB
validating  policy         all.policy.io   *.*        *
validating  policy         pods.policy.io  pods       CREATE
validating  policy         pods.policy.io  pods       CONNECT
validating  policy         pods.policy.io  pods/exec  CREATE
validating  policy         pods.policy.io  pods/exec  CONNECT
`,
			wantListed: []string{"validatingwebhookconfigurations"},
		},
		{
			name: "verb is case-insensitive and composes with resource",
			args: []string{"--verb", "update", "--resource", "pods"},
			wantOut: `TYPE        CONFIGURATION  WEBHOOK             RESOURCE  VERB
mutating    injector       inject.example.com  pods      UPDATE
validating  policy         all.policy.io       *.*       *
`,
			wantListed: []string{"mutatingwebhookconfigurations", "validatingwebhookconfigurations"},
		},
		{
			name:       "empty result",
			args:       []string{"--type", "mutating", "--verb", "DELETE"},
			wantStderr: "No webhooks found.\n",
			wantListed: []string{"mutatingwebhookconfigurations"},
		},
		{
			name: "summary shows both types even with zero counts",
			args: []string{"--summary", "--verb", "CONNECT"},
			wantOut: `TYPE        WEBHOOKS  RULES
mutating    0         0
validating  2         3
`,
			wantListed: []string{"mutatingwebhookconfigurations", "validatingwebhookconfigurations"},
		},
		{
			name: "summary with type shows only that type",
			args: []string{"--summary", "--type", "mutating"},
			wantOut: `TYPE      WEBHOOKS  RULES
mutating  1         3
`,
			wantListed: []string{"mutatingwebhookconfigurations"},
		},
		{
			name: "summary by resource",
			args: []string{"--summary", "--by-resource"},
			wantOut: `TYPE        RESOURCE          WEBHOOKS  RULES
mutating    deployments.apps  1         1
mutating    pods              1         2
validating  *.*               1         1
validating  pods              1         2
validating  pods/exec         1         2
`,
			wantListed: []string{"mutatingwebhookconfigurations", "validatingwebhookconfigurations"},
		},
		{
			name: "summary by verb",
			args: []string{"--summary", "--by-verb"},
			wantOut: `TYPE        VERB     WEBHOOKS  RULES
mutating    CREATE   1         2
mutating    UPDATE   1         1
validating  *        1         1
validating  CREATE   1         2
validating  CONNECT  1         2
`,
			wantListed: []string{"mutatingwebhookconfigurations", "validatingwebhookconfigurations"},
		},
		{
			name: "summary by resource and verb",
			args: []string{"--summary", "--by-resource", "--by-verb", "--resource", "pods"},
			wantOut: `TYPE        RESOURCE  VERB     WEBHOOKS  RULES
mutating    pods      CREATE   1         1
mutating    pods      UPDATE   1         1
validating  *.*       *        1         1
validating  pods      CREATE   1         1
validating  pods      CONNECT  1         1
`,
			wantListed: []string{"mutatingwebhookconfigurations", "validatingwebhookconfigurations"},
		},
		{
			name:       "summary breakdown with no matches",
			args:       []string{"--summary", "--by-resource", "--resource", "services", "--type", "mutating"},
			wantStderr: "No webhooks found.\n",
			wantListed: []string{"mutatingwebhookconfigurations"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newFakeClient()
			stdout, stderr, errText := run(t, client, tc.args...)
			if errText != tc.wantErr {
				t.Errorf("error = %q, want %q", errText, tc.wantErr)
			}
			if stdout != tc.wantOut {
				t.Errorf("stdout =\n%s\nwant\n%s", stdout, tc.wantOut)
			}
			if stderr != tc.wantStderr {
				t.Errorf("stderr = %q, want %q", stderr, tc.wantStderr)
			}

			var listed []string
			for _, a := range client.Actions() {
				if a.GetVerb() != "list" {
					t.Errorf("unexpected %s call on %s", a.GetVerb(), a.GetResource().Resource)
				}
				listed = append(listed, a.GetResource().Resource)
			}
			slices.Sort(listed)
			if !reflect.DeepEqual(listed, tc.wantListed) {
				t.Errorf("listed %v, want %v", listed, tc.wantListed)
			}
		})
	}
}

func TestKubeconfigFlagIsPassedToClient(t *testing.T) {
	var got string
	cmd := newCommand(build, func(kubeconfig string) (kubernetes.Interface, error) {
		got = kubeconfig
		return fake.NewClientset(), nil
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--kubeconfig", "/tmp/kc"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got != "/tmp/kc" {
		t.Errorf("kubeconfig = %q, want %q", got, "/tmp/kc")
	}
}

func TestNewClient(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "config")
	kubeconfig := `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: https://127.0.0.1:6443
users:
- name: u
contexts:
- name: ctx
  context:
    cluster: c
    user: u
current-context: ctx
`
	if err := os.WriteFile(valid, []byte(kubeconfig), 0o600); err != nil {
		t.Fatalf("writing kubeconfig: %v", err)
	}
	if _, err := newClient(valid); err != nil {
		t.Errorf("newClient(%q): %v", valid, err)
	}

	missing := filepath.Join(dir, "missing")
	_, err := newClient(missing)
	want := "loading kubeconfig: stat " + missing + ": no such file or directory"
	if err == nil || err.Error() != want {
		t.Errorf("newClient(%q) error = %v, want %q", missing, err, want)
	}
}

func TestHelpFlags(t *testing.T) {
	cmd := newCommand(build, nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) { got = append(got, f.Name) })
	want := []string{"by-resource", "by-verb", "help", "kubeconfig", "resource", "summary", "type", "verb", "version"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("flags = %v, want %v", got, want)
	}
}
