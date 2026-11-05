package webhooks

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// rule builds a webhook rule for the given groups, resources, and operations.
func rule(groups, resources []string, ops ...admissionregistrationv1.OperationType) admissionregistrationv1.RuleWithOperations {
	return admissionregistrationv1.RuleWithOperations{
		Operations: ops,
		Rule: admissionregistrationv1.Rule{
			APIGroups:   groups,
			APIVersions: []string{"v1"},
			Resources:   resources,
		},
	}
}

func mutating(name string, hooks ...admissionregistrationv1.MutatingWebhook) *admissionregistrationv1.MutatingWebhookConfiguration {
	return &admissionregistrationv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: name}, Webhooks: hooks}
}

func validating(name string, hooks ...admissionregistrationv1.ValidatingWebhook) *admissionregistrationv1.ValidatingWebhookConfiguration {
	return &admissionregistrationv1.ValidatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: name}, Webhooks: hooks}
}

func mhook(name string, rules ...admissionregistrationv1.RuleWithOperations) admissionregistrationv1.MutatingWebhook {
	return admissionregistrationv1.MutatingWebhook{Name: name, Rules: rules}
}

func vhook(name string, rules ...admissionregistrationv1.RuleWithOperations) admissionregistrationv1.ValidatingWebhook {
	return admissionregistrationv1.ValidatingWebhook{Name: name, Rules: rules}
}

const (
	create  = admissionregistrationv1.Create
	update  = admissionregistrationv1.Update
	del     = admissionregistrationv1.Delete
	connect = admissionregistrationv1.Connect
	all     = admissionregistrationv1.OperationAll
)

func TestList(t *testing.T) {
	objects := []runtime.Object{
		validating("policy",
			vhook("pods.policy.io",
				rule([]string{""}, []string{"pods"}, update, create),
				// Overlapping rule (another version) yields duplicates that are emitted once.
				rule([]string{""}, []string{"pods"}, create),
			),
			vhook("all.policy.io", rule([]string{"*"}, []string{"*/*"}, all)),
		),
		mutating("injector",
			mhook("inject.example.com",
				rule([]string{"apps", "extensions"}, []string{"deployments", "deployments/scale"}, connect, create),
			),
		),
		mutating("defaults",
			mhook("pods.defaults.io", rule([]string{""}, []string{"pods", "pods/exec"}, del, all)),
		),
	}
	wantMutating := []Row{
		{Mutating, "injector", "inject.example.com", "apps", "deployments", "CREATE"},
		{Mutating, "injector", "inject.example.com", "apps", "deployments", "CONNECT"},
		{Mutating, "injector", "inject.example.com", "extensions", "deployments", "CREATE"},
		{Mutating, "injector", "inject.example.com", "extensions", "deployments", "CONNECT"},
		{Mutating, "injector", "inject.example.com", "apps", "deployments/scale", "CREATE"},
		{Mutating, "injector", "inject.example.com", "apps", "deployments/scale", "CONNECT"},
		{Mutating, "injector", "inject.example.com", "extensions", "deployments/scale", "CREATE"},
		{Mutating, "injector", "inject.example.com", "extensions", "deployments/scale", "CONNECT"},
		{Mutating, "defaults", "pods.defaults.io", "", "pods", "*"},
		{Mutating, "defaults", "pods.defaults.io", "", "pods", "DELETE"},
		{Mutating, "defaults", "pods.defaults.io", "", "pods/exec", "*"},
		{Mutating, "defaults", "pods.defaults.io", "", "pods/exec", "DELETE"},
	}
	wantValidating := []Row{
		{Validating, "policy", "all.policy.io", "*", "*/*", "*"},
		{Validating, "policy", "pods.policy.io", "", "pods", "CREATE"},
		{Validating, "policy", "pods.policy.io", "", "pods", "UPDATE"},
	}
	cases := []struct {
		name  string
		types []Type
		want  []Row
		// wantListed are the resources the client was asked to LIST, in call order.
		wantListed []string
	}{
		{
			name:       "both types",
			types:      Types,
			want:       append(append([]Row{}, wantMutating...), wantValidating...),
			wantListed: []string{"mutatingwebhookconfigurations", "validatingwebhookconfigurations"},
		},
		{
			name:       "mutating only lists one kind",
			types:      []Type{Mutating},
			want:       wantMutating,
			wantListed: []string{"mutatingwebhookconfigurations"},
		},
		{
			name:       "validating only lists one kind",
			types:      []Type{Validating},
			want:       wantValidating,
			wantListed: []string{"validatingwebhookconfigurations"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewClientset(objects...)
			got, err := List(context.Background(), client, tc.types)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rows =\n%v\nwant\n%v", got, tc.want)
			}

			var listed []string
			for _, a := range client.Actions() {
				if a.GetVerb() != "list" {
					t.Errorf("unexpected %s call on %s", a.GetVerb(), a.GetResource().Resource)
				}
				listed = append(listed, a.GetResource().Resource)
			}
			// The two LIST calls may run concurrently, so compare them in sorted order.
			slices.Sort(listed)
			if !reflect.DeepEqual(listed, tc.wantListed) {
				t.Errorf("listed %v, want %v", listed, tc.wantListed)
			}
		})
	}
}

func TestListErrorNamesKind(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("list", "validatingwebhookconfigurations", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden")
	})
	_, err := List(context.Background(), client, Types)
	want := "listing validating webhook configurations: forbidden"
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestFilter(t *testing.T) {
	// row builds a row whose identity is its group, resource, and verb.
	row := func(group, resource, verb string) Row {
		return Row{Type: Validating, Configuration: "c", Webhook: "w", Group: group, Resource: resource, Verb: verb}
	}
	rows := []Row{
		row("", "pods", "CREATE"),
		row("", "pods", "*"),
		row("", "pods/exec", "CONNECT"),
		row("apps", "deployments", "UPDATE"),
		row("extensions", "deployments", "UPDATE"),
		row("*", "*", "DELETE"),
		row("*", "*/*", "CREATE"),
		row("", "*/exec", "CONNECT"),
		row("", "*/status", "UPDATE"),
	}
	cases := []struct {
		name     string
		verb     string
		resource string
		want     []Row
	}{
		{name: "no filters", want: rows},
		{
			name: "verb matches itself and wildcard",
			verb: "CREATE",
			want: []Row{row("", "pods", "CREATE"), row("", "pods", "*"), row("*", "*/*", "CREATE")},
		},
		{
			name:     "resource matches exact and covering wildcards",
			resource: "pods",
			want: []Row{
				row("", "pods", "CREATE"), row("", "pods", "*"), row("*", "*", "DELETE"), row("*", "*/*", "CREATE"),
			},
		},
		{
			name:     "subresource matches exact, */*, and */sub but not *",
			resource: "pods/exec",
			want:     []Row{row("", "pods/exec", "CONNECT"), row("*", "*/*", "CREATE"), row("", "*/exec", "CONNECT")},
		},
		{
			name:     "resource without group matches any group",
			resource: "deployments",
			want: []Row{
				row("apps", "deployments", "UPDATE"), row("extensions", "deployments", "UPDATE"),
				row("*", "*", "DELETE"), row("*", "*/*", "CREATE"),
			},
		},
		{
			name:     "resource with group matches that group or *",
			resource: "deployments.apps",
			want:     []Row{row("apps", "deployments", "UPDATE"), row("*", "*", "DELETE"), row("*", "*/*", "CREATE")},
		},
		{
			name:     "group with dots",
			resource: "deployments.networking.k8s.io",
			want:     []Row{row("*", "*", "DELETE"), row("*", "*/*", "CREATE")},
		},
		{
			name:     "filters compose with AND",
			verb:     "UPDATE",
			resource: "deployments.apps",
			want:     []Row{row("apps", "deployments", "UPDATE")},
		},
		{name: "no match", verb: "CONNECT", resource: "deployments", want: []Row{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Filter(slices.Clone(rows), tc.verb, tc.resource)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rows =\n%v\nwant\n%v", got, tc.want)
			}
		})
	}
}

func TestSummarize(t *testing.T) {
	rows := []Row{
		{Mutating, "inj", "a", "apps", "deployments", "CREATE"},
		{Mutating, "inj", "a", "apps", "deployments", "UPDATE"},
		{Mutating, "inj", "a", "", "pods", "CREATE"},
		{Mutating, "other", "a", "", "pods", "CREATE"},
		{Validating, "pol", "v", "", "pods", "*"},
		{Validating, "pol", "v", "", "pods", "DELETE"},
		{Validating, "pol", "w", "*", "*", "CREATE"},
	}
	cases := []struct {
		name       string
		rows       []Row
		types      []Type
		byResource bool
		byVerb     bool
		want       []Count
	}{
		{
			name:  "per type counts distinct webhooks by configuration",
			rows:  rows,
			types: Types,
			want:  []Count{{Type: Mutating, Webhooks: 2, Rules: 4}, {Type: Validating, Webhooks: 2, Rules: 3}},
		},
		{
			name:  "both types shown with zero counts",
			rows:  nil,
			types: Types,
			want:  []Count{{Type: Mutating}, {Type: Validating}},
		},
		{
			name:  "only the requested type",
			rows:  rows[4:],
			types: []Type{Validating},
			want:  []Count{{Type: Validating, Webhooks: 2, Rules: 3}},
		},
		{
			name:       "by resource",
			rows:       rows,
			types:      Types,
			byResource: true,
			want: []Count{
				{Type: Mutating, Resource: "deployments.apps", Webhooks: 1, Rules: 2},
				{Type: Mutating, Resource: "pods", Webhooks: 2, Rules: 2},
				{Type: Validating, Resource: "*.*", Webhooks: 1, Rules: 1},
				{Type: Validating, Resource: "pods", Webhooks: 1, Rules: 2},
			},
		},
		{
			name:   "by verb in API order",
			rows:   rows,
			types:  Types,
			byVerb: true,
			want: []Count{
				{Type: Mutating, Verb: "CREATE", Webhooks: 2, Rules: 3},
				{Type: Mutating, Verb: "UPDATE", Webhooks: 1, Rules: 1},
				{Type: Validating, Verb: "*", Webhooks: 1, Rules: 1},
				{Type: Validating, Verb: "CREATE", Webhooks: 1, Rules: 1},
				{Type: Validating, Verb: "DELETE", Webhooks: 1, Rules: 1},
			},
		},
		{
			name:       "by resource and verb",
			rows:       rows,
			types:      Types,
			byResource: true,
			byVerb:     true,
			want: []Count{
				{Type: Mutating, Resource: "deployments.apps", Verb: "CREATE", Webhooks: 1, Rules: 1},
				{Type: Mutating, Resource: "deployments.apps", Verb: "UPDATE", Webhooks: 1, Rules: 1},
				{Type: Mutating, Resource: "pods", Verb: "CREATE", Webhooks: 2, Rules: 2},
				{Type: Validating, Resource: "*.*", Verb: "CREATE", Webhooks: 1, Rules: 1},
				{Type: Validating, Resource: "pods", Verb: "*", Webhooks: 1, Rules: 1},
				{Type: Validating, Resource: "pods", Verb: "DELETE", Webhooks: 1, Rules: 1},
			},
		},
		{
			name:       "breakdown with no rows is empty",
			rows:       nil,
			types:      Types,
			byResource: true,
			want:       []Count{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Summarize(tc.rows, tc.types, tc.byResource, tc.byVerb)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("counts =\n%v\nwant\n%v", got, tc.want)
			}
		})
	}
}
