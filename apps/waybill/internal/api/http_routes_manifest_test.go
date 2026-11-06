package api

// http-routes.json (apps/waybill) is the published list of Waybill's routes.
// Enclii's native MCP (switchyard-api POST /mcp) reads it for its coverage
// contract and drift guard, because switchyard cannot import this module.
// These tests keep the file equal to the real router and its handlers: a new
// route, query parameter or response field fails here until the file says
// so, and then fails switchyard's guard until the MCP classifies it.

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

type routeManifest struct {
	Routes []struct {
		Method   string   `json:"method"`
		Path     string   `json:"path"`
		Query    []string `json:"query"`
		Response *struct {
			Fields []string            `json:"fields"`
			Items  map[string][]string `json:"items"`
		} `json:"response"`
	} `json:"routes"`
}

func loadRouteManifest(t *testing.T) routeManifest {
	t.Helper()
	raw, err := os.ReadFile("../../http-routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var m routeManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func TestHTTPRouteManifestMatchesTheRouter(t *testing.T) {
	loc, err := time.LoadLocation("America/Mexico_City")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(NewHandlers(nil, nil, zap.NewNop()), NewCostHandlers(nil, nil, zap.NewNop()), &ServerConfig{}, zap.NewNop(),
		NewResourceUsageHandlers(&fakeResourceReader{}, loc, zap.NewNop()))
	var registered, published []string
	for _, r := range s.Router().Routes() {
		registered = append(registered, r.Method+" "+r.Path)
	}
	for _, r := range loadRouteManifest(t).Routes {
		published = append(published, r.Method+" "+r.Path)
	}
	if got, want := sortedCopy(published), sortedCopy(registered); !reflect.DeepEqual(got, want) {
		t.Fatalf("http-routes.json lists\n  %v\nthe router registers\n  %v", got, want)
	}
}

// jsonShape lists a response type's JSON field names (anonymous embedded
// structs flattened, as encoding/json does) and, for each slice-of-struct
// field, the field names of its items.
func jsonShape(typ reflect.Type) ([]string, map[string][]string) {
	var fields []string
	items := map[string][]string{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if f.Anonymous && name == "" && f.Type.Kind() == reflect.Struct {
			sub, subItems := jsonShape(f.Type)
			fields = append(fields, sub...)
			for k, v := range subItems {
				items[k] = v
			}
			continue
		}
		if name == "-" || !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fields = append(fields, name)
		if f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Struct {
			sub, _ := jsonShape(f.Type.Elem())
			items[name] = sub
		}
	}
	return fields, items
}

func TestHTTPRouteManifestResponsesMatchTheHandlers(t *testing.T) {
	types := map[string]reflect.Type{
		"GET /api/v1/resource-usage/daily":       reflect.TypeOf(resourceUsageDailyResponse{}),
		"GET /api/v1/resource-usage/nodes/daily": reflect.TypeOf(nodeUsageDailyResponse{}),
	}
	seen := 0
	for _, r := range loadRouteManifest(t).Routes {
		if r.Response == nil {
			continue
		}
		key := r.Method + " " + r.Path
		typ, ok := types[key]
		if !ok {
			t.Errorf("%s publishes a response shape but this test does not know its type", key)
			continue
		}
		seen++
		fields, items := jsonShape(typ)
		if !reflect.DeepEqual(sortedCopy(r.Response.Fields), sortedCopy(fields)) {
			t.Errorf("%s fields: published %v, handler returns %v", key, r.Response.Fields, fields)
		}
		if len(r.Response.Items) != len(items) {
			t.Errorf("%s items: published %v, handler returns %v", key, r.Response.Items, items)
		}
		for name, want := range items {
			if !reflect.DeepEqual(sortedCopy(r.Response.Items[name]), sortedCopy(want)) {
				t.Errorf("%s %s[]: published %v, handler returns %v", key, name, r.Response.Items[name], want)
			}
		}
	}
	if seen != len(types) {
		t.Errorf("published %d response shapes, want %d", seen, len(types))
	}
}

// Every published query parameter is read by its handler (an invalid value
// is refused naming it), and the handlers read no parameter the file omits.
func TestHTTPRouteManifestQueryParamsMatchTheHandlers(t *testing.T) {
	invalid := map[string]string{"from": "2026-13-01", "to": "nope", "namespace": "Not_A_Namespace", "cluster": "-bad"}
	union := map[string]bool{}
	for _, r := range loadRouteManifest(t).Routes {
		for _, p := range r.Query {
			union[p] = true
			v, ok := invalid[p]
			if !ok {
				t.Errorf("%s %s: no invalid value known for query parameter %q", r.Method, r.Path, p)
				continue
			}
			w := get(resourceRouter(t, &fakeResourceReader{}), r.Path+"?"+p+"="+v)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), p) {
				t.Errorf("%s %s?%s=%s: status %d %s, want 400 naming %s", r.Method, r.Path, p, v, w.Code, w.Body, p)
			}
		}
	}

	src, err := os.ReadFile("resource_usage_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	read := map[string]bool{}
	for _, m := range regexp.MustCompile(`c\.Query\("([^"]+)"\)`).FindAllStringSubmatch(string(src), -1) {
		read[m[1]] = true
	}
	if !reflect.DeepEqual(read, union) {
		t.Errorf("the resource usage handlers read %v; http-routes.json publishes %v", read, union)
	}
}
