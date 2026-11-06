// Package webhooks lists admission webhook configurations and expands them
// into one Row per (webhook, resource, verb), with in-memory filtering and
// per-type summaries.
package webhooks

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Type is the kind of admission webhook.
type Type string

// Webhook types. As strings, mutating sorts before validating, which is also
// the order admission runs them in.
const (
	Mutating   Type = "mutating"
	Validating Type = "validating"
)

// Types is every webhook type, in admission order.
var Types = []Type{Mutating, Validating}

// Verbs are the admission operations a webhook rule can name, besides `*`.
var Verbs = []string{
	string(admissionregistrationv1.Create),
	string(admissionregistrationv1.Update),
	string(admissionregistrationv1.Delete),
	string(admissionregistrationv1.Connect),
}

// verbOrder is the API order verbs sort in, with `*` first.
var verbOrder = append([]string{string(admissionregistrationv1.OperationAll)}, Verbs...)

// Row is one (webhook, resource, verb) that a webhook intercepts.
type Row struct {
	Type          Type
	Configuration string
	Webhook       string
	// Group is the rule's API group as written; "" is the core group.
	Group string
	// Resource is the rule's resource as written, possibly with a subresource.
	Resource string
	// Verb is the rule's operation as written, e.g. CREATE or `*`.
	Verb string
}

// DisplayResource renders the resource kubectl-style: `resource` for the core
// group, `resource.group` otherwise.
func (r Row) DisplayResource() string {
	if r.Group == "" {
		return r.Resource
	}
	return r.Resource + "." + r.Group
}

// List fetches the webhook configurations of the given types, with one LIST
// call per type run concurrently, and returns their rows sorted and without
// duplicates.
func List(ctx context.Context, client kubernetes.Interface, types []Type) ([]Row, error) {
	results := make([][]Row, len(types))
	errs := make([]error, len(types))
	var wg sync.WaitGroup
	for i, t := range types {
		wg.Go(func() { results[i], errs[i] = list(ctx, client, t) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	rows := slices.Concat(results...)
	slices.SortFunc(rows, compareRows)
	return slices.Compact(rows), nil
}

// list fetches the configurations of one type and expands them into rows.
func list(ctx context.Context, client kubernetes.Interface, t Type) ([]Row, error) {
	api := client.AdmissionregistrationV1()
	var rows []Row
	switch t {
	case Mutating:
		configs, err := api.MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("listing mutating webhook configurations: %w", err)
		}
		for _, c := range configs.Items {
			for _, w := range c.Webhooks {
				rows = expand(rows, t, c.Name, w.Name, w.Rules)
			}
		}
	case Validating:
		configs, err := api.ValidatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("listing validating webhook configurations: %w", err)
		}
		for _, c := range configs.Items {
			for _, w := range c.Webhooks {
				rows = expand(rows, t, c.Name, w.Name, w.Rules)
			}
		}
	default:
		return nil, fmt.Errorf("unknown webhook type %q", t)
	}
	return rows, nil
}

// expand appends one row per (group, resource, operation) of each rule.
func expand(rows []Row, t Type, config, webhook string, rules []admissionregistrationv1.RuleWithOperations) []Row {
	for _, rule := range rules {
		for _, group := range rule.APIGroups {
			for _, resource := range rule.Resources {
				for _, op := range rule.Operations {
					rows = append(rows, Row{
						Type:          t,
						Configuration: config,
						Webhook:       webhook,
						Group:         group,
						Resource:      resource,
						Verb:          string(op),
					})
				}
			}
		}
	}
	return rows
}

// Filter keeps the rows matching both verb and resource, in place. An empty
// verb or resource matches every row.
//
// A verb matches rows for that verb or `*`. A resource such as `pods`,
// `pods/exec`, or `deployments.apps` matches rows for that resource or for a
// wildcard covering it; the group, when given, must match or be `*`.
func Filter(rows []Row, verb, resource string) []Row {
	name, group, hasGroup := strings.Cut(resource, ".")
	return slices.DeleteFunc(rows, func(r Row) bool {
		if verb != "" && r.Verb != verb && r.Verb != string(admissionregistrationv1.OperationAll) {
			return true
		}
		if resource == "" {
			return false
		}
		if hasGroup && r.Group != group && r.Group != "*" {
			return true
		}
		return !covers(r.Resource, name)
	})
}

// covers reports whether a rule resource covers the resource name: `*` covers
// any top-level resource, `*/*` covers anything, `x/*` covers any `x/sub` (but
// not `x`), and `*/sub` covers any `x/sub`.
func covers(rule, name string) bool {
	switch rule {
	case name, "*/*":
		return true
	case "*":
		return !strings.Contains(name, "/")
	}
	if ruleRes, ok := strings.CutSuffix(rule, "/*"); ok {
		res, sub, ok := strings.Cut(name, "/")
		return ok && res == ruleRes && sub != ""
	}
	ruleSub, ok := strings.CutPrefix(rule, "*/")
	if !ok {
		return false
	}
	_, sub, ok := strings.Cut(name, "/")
	return ok && sub == ruleSub
}

// Count is the number of distinct webhooks and of rows in one summary slice.
// Resource and Verb are set only when the summary is broken down by them.
type Count struct {
	Type     Type
	Resource string
	Verb     string
	Webhooks int
	Rules    int
}

// Summarize counts rows per type, optionally broken down by resource and/or
// verb, sorted by type, resource, then verb. Types are never combined. Without
// a breakdown every given type gets a Count, even with zero rows; with one,
// only slices that have rows appear. A webhook is identified by its
// configuration and name.
func Summarize(rows []Row, types []Type, byResource, byVerb bool) []Count {
	type sliceKey struct {
		typ            Type
		resource, verb string
	}
	type webhookKey struct {
		slice               sliceKey
		configuration, name string
	}
	counts := map[sliceKey]*Count{}
	if !byResource && !byVerb {
		for _, t := range types {
			counts[sliceKey{typ: t}] = &Count{Type: t}
		}
	}
	seen := map[webhookKey]bool{}
	for _, r := range rows {
		k := sliceKey{typ: r.Type}
		if byResource {
			k.resource = r.DisplayResource()
		}
		if byVerb {
			k.verb = r.Verb
		}
		c, ok := counts[k]
		if !ok {
			c = &Count{Type: k.typ, Resource: k.resource, Verb: k.verb}
			counts[k] = c
		}
		c.Rules++
		if w := (webhookKey{k, r.Configuration, r.Webhook}); !seen[w] {
			seen[w] = true
			c.Webhooks++
		}
	}

	result := make([]Count, 0, len(counts))
	for _, c := range counts {
		result = append(result, *c)
	}
	slices.SortFunc(result, func(a, b Count) int {
		return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.Resource, b.Resource), compareVerbs(a.Verb, b.Verb))
	})
	return result
}

// compareRows orders rows by type, resource, configuration, webhook, then verb.
func compareRows(a, b Row) int {
	return cmp.Or(
		cmp.Compare(a.Type, b.Type),
		cmp.Compare(a.DisplayResource(), b.DisplayResource()),
		cmp.Compare(a.Configuration, b.Configuration),
		cmp.Compare(a.Webhook, b.Webhook),
		compareVerbs(a.Verb, b.Verb),
	)
}

// compareVerbs orders verbs in API order rather than alphabetically.
func compareVerbs(a, b string) int {
	return cmp.Compare(slices.Index(verbOrder, a), slices.Index(verbOrder, b))
}
