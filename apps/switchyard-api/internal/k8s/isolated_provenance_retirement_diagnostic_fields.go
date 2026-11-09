package k8s

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
)

const retirementDiagnosticMaxFields = 24

var diagnosticJSONMarshaler = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

type retirementFieldWalker struct{ fields []string }

func retirementDiagnosticSpecFields(got, want *batchv1.JobSpec) ([]string, string, error) {
	actual, err := json.Marshal(got)
	if err != nil {
		return nil, "", retirementHold("diagnostic_spec_bound")
	}
	desired, err := json.Marshal(want)
	if err != nil {
		return nil, "", retirementHold("diagnostic_spec_bound")
	}
	kind := "field_mismatch"
	if bytes.Equal(actual, desired) {
		kind = "go_representation"
	}
	walker := retirementFieldWalker{fields: []string{}}
	if err = walker.walk(reflect.ValueOf(got), reflect.ValueOf(want), "spec", 0); err != nil {
		return nil, "", err
	}
	if len(walker.fields) == 0 {
		walker.fields = append(walker.fields, "spec")
	}
	return walker.fields, kind, nil
}

func (w *retirementFieldWalker) add(path string) error {
	if len(path) > 192 || len(w.fields) >= retirementDiagnosticMaxFields {
		return retirementHold("diagnostic_field_bound")
	}
	w.fields = append(w.fields, path)
	return nil
}

// Names come only from compiled Kubernetes JSON struct tags. Map keys and
// scalar/string-list values NEVER become identifiers; collection indexes are
// numeric and bounded. Unknown schemas or oversized collections fail closed.
func (w *retirementFieldWalker) walk(got, want reflect.Value, path string, depth int) error {
	if depth > 20 || !got.IsValid() || !want.IsValid() || got.Type() != want.Type() {
		return retirementHold("diagnostic_schema")
	}
	if reflect.DeepEqual(got.Interface(), want.Interface()) {
		return nil
	}
	if got.Kind() == reflect.Pointer {
		if got.IsNil() || want.IsNil() {
			return w.add(path)
		}
		return w.walk(got.Elem(), want.Elem(), path, depth+1)
	}
	if got.Type().Implements(diagnosticJSONMarshaler) {
		return w.add(path)
	}
	switch got.Kind() {
	case reflect.Struct:
		traversed := false
		for i := 0; i < got.NumField(); i++ {
			field := got.Type().Field(i)
			if field.PkgPath != "" {
				continue
			}
			tag := field.Tag.Get("json")
			name := strings.Split(tag, ",")[0]
			if name == "-" {
				continue
			}
			if name == "" && field.Anonymous {
				if err := w.walk(got.Field(i), want.Field(i), path, depth+1); err != nil {
					return err
				}
				traversed = true
				continue
			}
			if !retirementDiagnosticFieldName(name) {
				return retirementHold("diagnostic_schema")
			}
			if err := w.walk(got.Field(i), want.Field(i), path+"."+name, depth+1); err != nil {
				return err
			}
			traversed = true
		}
		if !traversed {
			return w.add(path)
		}
	case reflect.Slice, reflect.Array:
		if got.Len() > 64 || want.Len() > 64 {
			return retirementHold("diagnostic_collection_bound")
		}
		element := got.Type().Elem().Kind()
		if element != reflect.Struct && element != reflect.Pointer {
			return w.add(path)
		}
		if got.Len() != want.Len() {
			if err := w.add(path); err != nil {
				return err
			}
		}
		for i := 0; i < got.Len() && i < want.Len(); i++ {
			if err := w.walk(got.Index(i), want.Index(i), path+"["+strconv.Itoa(i)+"]", depth+1); err != nil {
				return err
			}
		}
	default:
		return w.add(path)
	}
	return nil
}

func retirementDiagnosticFieldName(name string) bool {
	if len(name) == 0 || len(name) > 64 || !((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z')) {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
