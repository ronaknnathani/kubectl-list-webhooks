package output

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/ronaknnathani/kubectl-list-webhooks/pkg/webhooks"
)

var rows = []webhooks.Row{
	{Type: webhooks.Mutating, Configuration: "injector", Webhook: "inject.example.com", Group: "apps", Resource: "deployments", Verb: "CREATE"},
	{Type: webhooks.Mutating, Configuration: "injector", Webhook: "inject.example.com", Group: "", Resource: "pods", Verb: "CREATE"},
	{Type: webhooks.Mutating, Configuration: "injector", Webhook: "inject.example.com", Group: "", Resource: "pods", Verb: "UPDATE"},
	{Type: webhooks.Validating, Configuration: "policy", Webhook: "all.policy.io", Group: "*", Resource: "*", Verb: "*"},
	{Type: webhooks.Validating, Configuration: "policy", Webhook: "pods.policy.io", Group: "", Resource: "pods", Verb: "CREATE"},
}

func TestListingPlain(t *testing.T) {
	var buf bytes.Buffer
	if err := Listing(&buf, rows, false); err != nil {
		t.Fatalf("Listing: %v", err)
	}
	want := `TYPE        CONFIGURATION  WEBHOOK             RESOURCE          VERB
mutating    injector       inject.example.com  deployments.apps  CREATE
mutating    injector       inject.example.com  pods              CREATE
mutating    injector       inject.example.com  pods              UPDATE
validating  policy         all.policy.io       *.*               *
validating  policy         pods.policy.io      pods              CREATE
`
	if got := buf.String(); got != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

// TestListingColor is the one golden case for raw escape sequences.
func TestListingColor(t *testing.T) {
	var buf bytes.Buffer
	if err := Listing(&buf, rows, true); err != nil {
		t.Fatalf("Listing: %v", err)
	}
	want := "\x1b[1mTYPE        CONFIGURATION  WEBHOOK             RESOURCE          VERB\x1b[0m\n" +
		"\x1b[38;5;170mmutating    injector       inject.example.com  deployments.apps  CREATE\x1b[0m\n" +
		"\x1b[38;5;133mmutating    injector       inject.example.com  pods              CREATE\x1b[0m\n" +
		"\x1b[38;5;133mmutating    injector       inject.example.com  pods              UPDATE\x1b[0m\n" +
		"\x1b[38;5;37mvalidating  policy         all.policy.io       *.*               *\x1b[0m\n" +
		"\x1b[38;5;31mvalidating  policy         pods.policy.io      pods              CREATE\x1b[0m\n"
	if got := buf.String(); got != want {
		t.Errorf("output =\n%q\nwant\n%q", got, want)
	}
}

func TestSummaryPlain(t *testing.T) {
	cases := []struct {
		name       string
		counts     []webhooks.Count
		byResource bool
		byVerb     bool
		want       string
	}{
		{
			name: "per type",
			counts: []webhooks.Count{
				{Type: webhooks.Mutating, Webhooks: 4, Rules: 31},
				{Type: webhooks.Validating, Webhooks: 7, Rules: 58},
			},
			want: `TYPE        WEBHOOKS  RULES
mutating    4         31
validating  7         58
`,
		},
		{
			name: "by resource",
			counts: []webhooks.Count{
				{Type: webhooks.Mutating, Resource: "deployments.apps", Webhooks: 1, Rules: 2},
				{Type: webhooks.Mutating, Resource: "pods", Webhooks: 3, Rules: 9},
				{Type: webhooks.Validating, Resource: "pods", Webhooks: 5, Rules: 12},
			},
			byResource: true,
			want: `TYPE        RESOURCE          WEBHOOKS  RULES
mutating    deployments.apps  1         2
mutating    pods              3         9
validating  pods              5         12
`,
		},
		{
			name: "by verb",
			counts: []webhooks.Count{
				{Type: webhooks.Mutating, Verb: "CREATE", Webhooks: 3, Rules: 4},
				{Type: webhooks.Mutating, Verb: "UPDATE", Webhooks: 1, Rules: 2},
				{Type: webhooks.Validating, Verb: "CREATE", Webhooks: 4, Rules: 5},
				{Type: webhooks.Validating, Verb: "UPDATE", Webhooks: 4, Rules: 5},
				{Type: webhooks.Validating, Verb: "DELETE", Webhooks: 1, Rules: 1},
			},
			byVerb: true,
			want: `TYPE        VERB    WEBHOOKS  RULES
mutating    CREATE  3         4
mutating    UPDATE  1         2
validating  CREATE  4         5
validating  UPDATE  4         5
validating  DELETE  1         1
`,
		},
		{
			name: "by resource and verb",
			counts: []webhooks.Count{
				{Type: webhooks.Mutating, Resource: "pods", Verb: "CREATE", Webhooks: 3, Rules: 4},
				{Type: webhooks.Validating, Resource: "*.*", Verb: "*", Webhooks: 1, Rules: 1},
			},
			byResource: true,
			byVerb:     true,
			want: `TYPE        RESOURCE  VERB    WEBHOOKS  RULES
mutating    pods      CREATE  3         4
validating  *.*       *       1         1
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Summary(&buf, tc.counts, tc.byResource, tc.byVerb, false); err != nil {
				t.Fatalf("Summary: %v", err)
			}
			if got := buf.String(); got != tc.want {
				t.Errorf("output =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestBands(t *testing.T) {
	m, v := webhooks.Mutating, webhooks.Validating
	cases := []struct {
		name  string
		lines []line
		want  []int
	}{
		{
			name: "alternates on group change and restarts per type",
			lines: []line{
				{typ: m, group: "deployments.apps"},
				{typ: m, group: "pods"},
				{typ: m, group: "pods"},
				{typ: m, group: "services"},
				{typ: v, group: "services"},
				{typ: v, group: "*.*"},
			},
			want: []int{0, 1, 1, 0, 0, 1},
		},
		{
			name:  "no group key means no banding",
			lines: []line{{typ: m}, {typ: m}, {typ: v}},
			want:  []int{0, 0, 0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bands(tc.lines); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("bands = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSummaryBandKeys checks which column each summary form bands on.
func TestSummaryBandKeys(t *testing.T) {
	counts := []webhooks.Count{
		{Type: webhooks.Mutating, Resource: "pods", Verb: "CREATE"},
		{Type: webhooks.Mutating, Resource: "pods", Verb: "UPDATE"},
		{Type: webhooks.Mutating, Resource: "services", Verb: "UPDATE"},
	}
	cases := []struct {
		name       string
		byResource bool
		byVerb     bool
		want       []string
	}{
		{name: "per type", want: []string{"", "", ""}},
		{name: "by resource", byResource: true, want: []string{"pods", "pods", "services"}},
		{name: "by verb", byVerb: true, want: []string{"CREATE", "UPDATE", "UPDATE"}},
		{name: "by resource and verb", byResource: true, byVerb: true, want: []string{"pods", "pods", "services"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, lines := summaryTable(counts, tc.byResource, tc.byVerb)
			var got []string
			for _, l := range lines {
				got = append(got, l.group)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("groups = %v, want %v", got, tc.want)
			}
		})
	}
}
