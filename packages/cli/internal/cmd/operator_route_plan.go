package cmd

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// printRoutePlan renders a tunnels-apply plan as a table ahead of the raw
// data, so the label column ("REPOINT (blocked)", "SKIP", ...) is the first
// thing an operator reads. Any response without such a plan prints nothing.
func printRoutePlan(out io.Writer, data any) {
	fields, ok := data.(map[string]any)
	if !ok {
		return
	}
	plan, ok := fields["plan"].([]any)
	if !ok || len(plan) == 0 {
		return
	}
	rows := make([]map[string]any, 0, len(plan))
	for _, entry := range plan {
		row, ok := entry.(map[string]any)
		if !ok || row["label"] == nil || row["hostname"] == nil {
			return
		}
		rows = append(rows, row)
	}

	fmt.Fprintln(out, "\nRoutes:")
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "LABEL\tHOSTNAME\tLIVE\tDESIRED\tENVIRONMENT")
	for _, row := range rows {
		env := stringField(row, "environment")
		if source := stringField(row, "environment_source"); source != "" {
			env += " (" + source + ")"
		}
		live := stringField(row, "current_service")
		if live == "" {
			live = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", stringField(row, "label"), stringField(row, "hostname"),
			live, stringField(row, "desired_service"), env)
	}
	_ = tw.Flush()
}

func stringField(row map[string]any, key string) string {
	if value, ok := row[key].(string); ok {
		return value
	}
	return ""
}
