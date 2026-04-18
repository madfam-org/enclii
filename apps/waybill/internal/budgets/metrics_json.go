package budgets

import "encoding/json"

// decodeMetricsJSON returns the metrics jsonb as a map[string]float64,
// defaulting to an empty map on any parse failure. Numeric coercion is
// permissive so legacy events with int or string values still yield
// usable numbers.
func decodeMetricsJSON(raw []byte) map[string]float64 {
	out := map[string]float64{}
	if len(raw) == 0 {
		return out
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return out
	}
	for k, v := range parsed {
		switch n := v.(type) {
		case float64:
			out[k] = n
		case float32:
			out[k] = float64(n)
		case int:
			out[k] = float64(n)
		case int64:
			out[k] = float64(n)
		case json.Number:
			if f, err := n.Float64(); err == nil {
				out[k] = f
			}
		}
	}
	return out
}
