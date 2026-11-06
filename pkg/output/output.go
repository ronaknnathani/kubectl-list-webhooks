// Package output renders webhook rows and summaries as kubectl-style tables.
//
// With color on, every row takes its webhook type's hue, and rows alternate
// between two shades of that hue each time their group key (resource, or verb)
// changes, so each group reads as one block. Color wraps whole lines after
// column alignment, so the uncolored output is byte-for-byte the plain table.
package output

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/ronaknnathani/kubectl-list-webhooks/pkg/webhooks"
)

// shades are two 256-color foregrounds per type, readable on dark and light
// backgrounds: magenta for mutating, cyan for validating.
var shades = map[webhooks.Type][2]int{
	webhooks.Mutating:   {170, 133},
	webhooks.Validating: {37, 31},
}

const (
	bold  = "\x1b[1m"
	reset = "\x1b[0m"
)

// line is one table row: its cells, the type that picks its hue, and the
// group key it is banded by ("" for no banding).
type line struct {
	typ   webhooks.Type
	group string
	cells []string
}

// Listing writes one row per (webhook, resource, verb), banded by resource.
func Listing(w io.Writer, rows []webhooks.Row, color bool) error {
	lines := make([]line, 0, len(rows))
	for _, r := range rows {
		resource := r.DisplayResource()
		lines = append(lines, line{
			typ:   r.Type,
			group: resource,
			cells: []string{string(r.Type), r.Configuration, r.Webhook, resource, r.Verb},
		})
	}
	return render(w, []string{"TYPE", "CONFIGURATION", "WEBHOOK", "RESOURCE", "VERB"}, lines, color)
}

// Summary writes per-type counts, with RESOURCE and VERB columns when the
// counts are broken down by them.
func Summary(w io.Writer, counts []webhooks.Count, byResource, byVerb, color bool) error {
	header, lines := summaryTable(counts, byResource, byVerb)
	return render(w, header, lines, color)
}

// summaryTable builds the summary header and rows. Rows are banded by
// resource when that column is shown, otherwise by verb when that is shown.
func summaryTable(counts []webhooks.Count, byResource, byVerb bool) (header []string, lines []line) {
	header = []string{"TYPE"}
	if byResource {
		header = append(header, "RESOURCE")
	}
	if byVerb {
		header = append(header, "VERB")
	}
	header = append(header, "WEBHOOKS", "RULES")

	for _, c := range counts {
		l := line{typ: c.Type, cells: []string{string(c.Type)}}
		switch {
		case byResource:
			l.group = c.Resource
		case byVerb:
			l.group = c.Verb
		}
		if byResource {
			l.cells = append(l.cells, c.Resource)
		}
		if byVerb {
			l.cells = append(l.cells, c.Verb)
		}
		l.cells = append(l.cells, strconv.Itoa(c.Webhooks), strconv.Itoa(c.Rules))
		lines = append(lines, l)
	}
	return header, lines
}

// render aligns the header and lines into columns, colors each whole line when
// color is set, and writes the result to w.
func render(w io.Writer, header []string, lines []line, color bool) error {
	var table bytes.Buffer
	tw := tabwriter.NewWriter(&table, 0, 8, 2, ' ', 0)
	rows := [][]string{header}
	for _, l := range lines {
		rows = append(rows, l.cells)
	}
	for _, cells := range rows {
		if _, err := fmt.Fprintln(tw, strings.Join(cells, "\t")); err != nil {
			return fmt.Errorf("aligning table: %w", err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("aligning table: %w", err)
	}

	out := table.String()
	if color {
		out = colorize(out, lines)
	}
	if _, err := io.WriteString(w, out); err != nil {
		return fmt.Errorf("writing table: %w", err)
	}
	return nil
}

// colorize wraps the aligned header line in bold and each row line in its
// type's shade for its band.
func colorize(table string, lines []line) string {
	formatted := strings.Split(strings.TrimSuffix(table, "\n"), "\n")
	band := bands(lines)
	var b strings.Builder
	b.WriteString(bold + formatted[0] + reset + "\n")
	for i, l := range lines {
		fmt.Fprintf(&b, "\x1b[38;5;%dm%s%s\n", shades[l.typ][band[i]], formatted[i+1], reset)
	}
	return b.String()
}

// bands assigns each line a shade index (0 or 1) that flips whenever the group
// key changes within a type and restarts at 0 for each type.
func bands(lines []line) []int {
	band := make([]int, len(lines))
	for i := 1; i < len(lines); i++ {
		prev, cur := lines[i-1], lines[i]
		switch {
		case cur.typ != prev.typ:
			band[i] = 0
		case cur.group != prev.group:
			band[i] = 1 - band[i-1]
		default:
			band[i] = band[i-1]
		}
	}
	return band
}
