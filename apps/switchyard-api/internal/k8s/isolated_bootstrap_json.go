package k8s

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

// Closed bounded UTF-8 JSON, including duplicate-key rejection at every level.
// Error text intentionally excludes the input and decoder diagnostics.
func DecodeInitialBootstrapJSON(b []byte, out any) error {
	fail := fmt.Errorf("initial bounded JSON refused")
	if len(b) == 0 || len(b) > initialRecordLimit || !utf8.Valid(b) {
		return fail
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var walk func() error
	walk = func() error {
		t, e := d.Token()
		if e != nil {
			return fail
		}
		if del, ok := t.(json.Delim); ok {
			switch del {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					key, ok := k.(string)
					if e != nil || !ok || seen[key] {
						return fail
					}
					seen[key] = true
					if walk() != nil {
						return fail
					}
				}
				end, e := d.Token()
				if e != nil || end != json.Delim('}') {
					return fail
				}
			case '[':
				for d.More() {
					if walk() != nil {
						return fail
					}
				}
				end, e := d.Token()
				if e != nil || end != json.Delim(']') {
					return fail
				}
			default:
				return fail
			}
		}
		return nil
	}
	if walk() != nil {
		return fail
	}
	if _, e := d.Token(); e != io.EOF {
		return fail
	}
	clean := bytes.TrimSpace(b)
	if len(clean) == 0 || clean[0] != '{' {
		return fail
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return fail
	}
	return nil
}
