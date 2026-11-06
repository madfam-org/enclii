package waybillmcp

// JSON Schemas of the tools. The output schemas are also the projection
// (tools.go project): a Waybill field reaches MCP callers only when it is
// listed here, and the drift guard (coverage_test.go) keeps these lists equal
// to the response fields apps/waybill/http-routes.json publishes.

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func date(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "pattern": `^\d{4}-\d{2}-\d{2}$`}
}

func num(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }

func nullableNum(desc string) map[string]any {
	return map[string]any{"type": []string{"number", "null"}, "description": desc + " (null when not measured)"}
}

func nullableInt(desc string) map[string]any {
	return map[string]any{"type": []string{"integer", "null"}, "description": desc + " (null when not measured)"}
}

func integer(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func arrayOf(items map[string]any, desc string) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": items}
}

func rangeInputs(withNamespace bool) map[string]any {
	props := map[string]any{
		"from":    date("First civil day, inclusive (YYYY-MM-DD, the ledger's time zone)"),
		"to":      date("Last civil day, inclusive; at most 62 days after from"),
		"cluster": str("Only this cluster's rows (the ledger has one cluster today)"),
	}
	if withNamespace {
		props["namespace"] = map[string]any{"type": "string", "description": "Only this Kubernetes namespace",
			"pattern": `^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`}
	}
	return props
}

func resourceDailyInputSchema() map[string]any { return obj(rangeInputs(true), "from", "to") }

func nodeDailyInputSchema() map[string]any { return obj(rangeInputs(false), "from", "to") }

func captureSchema() map[string]any {
	return obj(map[string]any{
		"date":              date("Civil day"),
		"cluster":           str("Cluster"),
		"usage_coverage":    num("Share of the day Prometheus held usage samples for (0-1)"),
		"requests_coverage": num("Share of the day Prometheus held request samples for (0-1)"),
		"workload_rows":     integer("Workload rows written for the day"),
		"node_rows":         integer("Node rows written for the day"),
		"source":            str("Where the day was read from"),
		"captured_at":       map[string]any{"type": "string", "format": "date-time", "description": "When the aggregator committed the day"},
	}, "date", "cluster", "usage_coverage", "requests_coverage", "workload_rows", "node_rows", "source", "captured_at")
}

func meta(rowsKey string, rows map[string]any, rowsDesc string) map[string]any {
	return obj(map[string]any{
		"from":             date("First day of the range"),
		"to":               date("Last day of the range"),
		"timezone":         str("The ledger's civil-day time zone"),
		"captures":         arrayOf(captureSchema(), "The days the aggregator committed, with their coverage"),
		"uncaptured_dates": arrayOf(date("Civil day"), "Days in range with no capture: not captured yet or nothing held; not zeros"),
		rowsKey:            arrayOf(rows, rowsDesc),
	}, "from", "to", "timezone", "captures", "uncaptured_dates", rowsKey)
}

func resourceDailyOutputSchema() map[string]any {
	return meta("rows", obj(map[string]any{
		"date":                      date("Civil day"),
		"cluster":                   str("Cluster"),
		"node":                      str("Node the workload ran on"),
		"namespace":                 str("Kubernetes namespace"),
		"workload_kind":             str("Owner kind (Deployment, StatefulSet, Job, ...); runner namespaces aggregate per namespace"),
		"workload_name":             str("Owner name"),
		"container":                 str("Container"),
		"pod_count":                 integer("Distinct pods seen"),
		"cpu_core_hours_used":       nullableNum("CPU core-hours used"),
		"cpu_core_hours_requested":  nullableNum("CPU core-hours requested"),
		"cpu_limit_core_hours":      nullableNum("CPU limit, core-hours"),
		"cpu_cores_p95":             nullableNum("CPU cores, 95th percentile"),
		"cpu_cores_max":             nullableNum("CPU cores, maximum"),
		"mem_gib_hours_used":        nullableNum("Memory GiB-hours used (working set)"),
		"mem_gib_hours_requested":   nullableNum("Memory GiB-hours requested"),
		"mem_limit_gib_hours":       nullableNum("Memory limit, GiB-hours"),
		"mem_working_set_p95_bytes": nullableInt("Memory working set, 95th percentile, bytes"),
		"mem_working_set_max_bytes": nullableInt("Memory working set, maximum, bytes"),
		"sample_count":              integer("Samples behind the row"),
		"coverage":                  num("Share of the day the row has samples for (0-1)"),
		"source":                    str("Where the row was read from"),
	}, "date", "cluster", "node", "namespace", "workload_kind", "workload_name", "container", "pod_count", "sample_count", "coverage", "source"),
		"One row per day, node, namespace, workload and container")
}

func nodeDailyOutputSchema() map[string]any {
	return meta("nodes", obj(map[string]any{
		"date":                       date("Civil day"),
		"cluster":                    str("Cluster"),
		"node":                       str("Node"),
		"pod_count":                  integer("Distinct pods seen on the node"),
		"cpu_allocatable_core_hours": nullableNum("Allocatable CPU core-hours"),
		"cpu_core_hours_requested":   nullableNum("CPU core-hours requested by its workloads"),
		"cpu_core_hours_used":        nullableNum("CPU core-hours used by its workloads"),
		"cpu_cores_p95":              nullableNum("CPU cores, 95th percentile"),
		"cpu_cores_max":              nullableNum("CPU cores, maximum"),
		"mem_allocatable_gib_hours":  nullableNum("Allocatable memory GiB-hours"),
		"mem_gib_hours_requested":    nullableNum("Memory GiB-hours requested by its workloads"),
		"mem_gib_hours_used":         nullableNum("Memory GiB-hours used by its workloads"),
		"mem_working_set_max_bytes":  nullableInt("Memory working set, maximum, bytes"),
		"sample_count":               integer("Samples behind the row"),
		"coverage":                   num("Share of the day the row has samples for (0-1)"),
		"source":                     str("Where the row was read from"),
	}, "date", "cluster", "node", "pod_count", "sample_count", "coverage", "source"),
		"One row per day and node")
}
