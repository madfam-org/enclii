package lockbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
)

const trainingCapabilityBodyLimit = 16 * 1024
const trainingCapabilityTimeout = 10 * time.Second

// No Vault response, address, namespace or credential enters this error.
var ErrTrainingCapabilitiesUnverified = errors.New("training capability response unverified")

// TrainingCustodyCapabilityPaths derives the closed destinations from the
// reviewed intake registry. New destinations require a separate source review.
func TrainingCustodyCapabilityPaths() ([]string, error) {
	registry, err := secretsintake.LoadRegistry()
	if err != nil {
		return nil, ErrTrainingCapabilitiesUnverified
	}
	return trainingCapabilityPaths(registry)
}

func trainingCapabilityPaths(registry map[string]secretsintake.Target) ([]string, error) {
	type destination struct{ path, namespace string }
	expected := map[string]destination{
		"crea-sim/ui-oidc":             {"secret/crea-sim-gateway", "crea-sim"},
		"crea-sim/mcp-oidc":            {"secret/crea-sim-gateway", "crea-sim"},
		"crea-sim/selva-oidc":          {"secret/crea-sim-selva", "crea-sim"},
		"crea-sim/gateway-runtime":     {"secret/crea-sim-gateway", "crea-sim"},
		"crea-sim/capture-runtime":     {"secret/crea-sim-captura", "crea-sim"},
		"crea-sim/kernel01-runtime":    {"secret/crea-sim-map01", "crea-sim"},
		"crea-sim/kernel02-runtime":    {"secret/crea-sim-map01", "crea-sim"}, // shares kernel 01's path, prefixed keys
		"crea-sim/kernel03-runtime":    {"secret/crea-sim-map01", "crea-sim"}, // shares kernel 01's path, prefixed keys
		"crea-sim/gateway-ca":          {"secret/crea-sim-gateway", "crea-sim"},
		"crea-sim/capture-ca":          {"secret/crea-sim-captura", "crea-sim"},
		"crea-sim/kernel01-ca":         {"secret/crea-sim-map01", "crea-sim"},
		"crea-sim/kernel02-ca":         {"secret/crea-sim-map01", "crea-sim"},
		"crea-sim/kernel03-ca":         {"secret/crea-sim-map01", "crea-sim"},
		"janua-staging/runtime-crypto": {"secret/janua-staging", "janua-staging"},
		"janua-staging/database":       {"secret/janua-staging", "janua-staging"},
		"janua-staging/database-ca":    {"secret/janua-staging", "janua-staging"},
		"janua-staging/redis":          {"secret/janua-staging", "janua-staging"},
	}
	seen := make(map[string]bool, len(expected))
	destinations := make(map[string]bool, 5)
	for id, target := range registry {
		if !strings.HasPrefix(id, "crea-sim/") && !strings.HasPrefix(id, "janua-staging/") {
			continue
		}
		want, ok := expected[id]
		if !ok || target.ID != id || want.path != target.VaultPath || want.namespace != target.Namespace {
			return nil, ErrTrainingCapabilitiesUnverified
		}
		seen[id] = true
		destinations[target.VaultPath] = true
	}
	if len(seen) != len(expected) || len(destinations) != 5 {
		return nil, ErrTrainingCapabilitiesUnverified
	}
	paths := make([]string, 0, 10)
	for logicalPath := range destinations {
		name := strings.TrimPrefix(logicalPath, "secret/")
		paths = append(paths, "secret/data/"+name, "secret/metadata/"+name)
	}
	sort.Strings(paths)
	return paths, nil
}

// ValidateTrainingCustodyCapabilities also bounds optional API implementations.
// Known elevated capabilities are preserved for an explicit scope refusal;
// unknown strings, duplicates and incomplete maps are never returned.
func ValidateTrainingCustodyCapabilities(capabilities map[string][]string) (map[string][]string, error) {
	paths, err := TrainingCustodyCapabilityPaths()
	if err != nil {
		return nil, err
	}
	return validateTrainingCapabilities(paths, capabilities)
}

func validateTrainingCapabilities(paths []string, capabilities map[string][]string) (map[string][]string, error) {
	if len(capabilities) != len(paths) {
		return nil, ErrTrainingCapabilitiesUnverified
	}
	allowed := map[string]bool{"create": true, "read": true, "update": true, "patch": true,
		"delete": true, "list": true, "sudo": true, "root": true, "deny": true}
	validated := make(map[string][]string, len(paths))
	for _, path := range paths {
		values, found := capabilities[path]
		if !found || len(values) == 0 || len(values) > len(allowed) {
			return nil, ErrTrainingCapabilitiesUnverified
		}
		seen := make(map[string]bool, len(values))
		for _, capability := range values {
			if !allowed[capability] || seen[capability] {
				return nil, ErrTrainingCapabilitiesUnverified
			}
			seen[capability] = true
		}
		if seen["deny"] && len(values) != 1 {
			return nil, ErrTrainingCapabilitiesUnverified
		}
		validated[path] = append([]string(nil), values...)
		sort.Strings(validated[path])
	}
	return validated, nil
}

// ProbeTrainingCustodyCapabilities uses the current writer's own identity once.
// It never accesses KV values, applies policy or accepts a caller-selected path.
func (v *VaultClient) ProbeTrainingCustodyCapabilities(ctx context.Context) (map[string][]string, error) {
	if v == nil || !v.enabled || v.token == "" || v.httpClient == nil {
		return nil, ErrTrainingCapabilitiesUnverified
	}
	paths, err := TrainingCustodyCapabilityPaths()
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(v.address)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil ||
		(base.Path != "" && base.Path != "/") || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" {
		return nil, ErrTrainingCapabilitiesUnverified
	}
	base.Path = "/v1/sys/capabilities-self"
	base.RawPath = ""
	body, _ := json.Marshal(struct {
		Paths []string `json:"paths"`
	}{paths})
	bounded, cancel := context.WithTimeout(ctx, trainingCapabilityTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(bounded, http.MethodPost, base.String(), bytes.NewReader(body))
	if err != nil {
		return nil, ErrTrainingCapabilitiesUnverified
	}
	request.GetBody = nil // Do not offer the transport a replayable request body.
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Vault-Token", v.token)
	if v.namespace != "" {
		request.Header.Set("X-Vault-Namespace", v.namespace)
	}
	// Clone; do not change legacy client redirect/timeout behavior.
	client := *v.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Jar = nil
	if client.Timeout <= 0 || client.Timeout > trainingCapabilityTimeout {
		client.Timeout = trainingCapabilityTimeout
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, ErrTrainingCapabilitiesUnverified
	}
	defer response.Body.Close()
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || err != nil || mediaType != "application/json" {
		return nil, ErrTrainingCapabilitiesUnverified
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, trainingCapabilityBodyLimit+1))
	if err != nil || len(raw) > trainingCapabilityBodyLimit {
		return nil, ErrTrainingCapabilitiesUnverified
	}
	return parseTrainingCapabilities(raw, paths)
}

// Vault1.18 injects sys capabilities into the top level as well as data.
// Accept documented bare data, the normal envelope or its complete injected
// form; duplicate JSON keys and conflicting/partial injected data fail closed.
func parseTrainingCapabilities(raw []byte, paths []string) (map[string][]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := uniqueTrainingJSON(decoder, 0)
	if err != nil {
		return nil, ErrTrainingCapabilitiesUnverified
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, ErrTrainingCapabilitiesUnverified
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, ErrTrainingCapabilitiesUnverified
	}
	data := object
	if nested, enveloped := object["data"]; enveloped {
		data, ok = nested.(map[string]any)
		if !ok {
			return nil, ErrTrainingCapabilitiesUnverified
		}
		injected := make(map[string]any)
		for key, val := range object {
			switch key {
			case "data":
			case "request_id":
				id, valid := val.(string)
				if !valid || len(id) > 255 {
					return nil, ErrTrainingCapabilitiesUnverified
				}
			case "lease_id":
				if val != "" {
					return nil, ErrTrainingCapabilitiesUnverified
				}
			case "lease_duration":
				if val != json.Number("0") {
					return nil, ErrTrainingCapabilitiesUnverified
				}
			case "renewable":
				if val != false {
					return nil, ErrTrainingCapabilitiesUnverified
				}
			case "auth", "wrap_info":
				if val != nil {
					return nil, ErrTrainingCapabilitiesUnverified
				}
			case "warnings":
				if val != nil {
					warnings, valid := val.([]any)
					if !valid || len(warnings) != 0 {
						return nil, ErrTrainingCapabilitiesUnverified
					}
				}
			case "mount_type":
				if val != "" && val != "system" {
					return nil, ErrTrainingCapabilitiesUnverified
				}
			default:
				injected[key] = val
			}
		}
		if len(injected) > 0 {
			a, errA := trainingCapabilityArrays(injected, paths)
			b, errB := trainingCapabilityArrays(data, paths)
			if errA != nil || errB != nil || !reflect.DeepEqual(a, b) {
				return nil, ErrTrainingCapabilitiesUnverified
			}
		}
	}
	return trainingCapabilityArrays(data, paths)
}

func trainingCapabilityArrays(data map[string]any, paths []string) (map[string][]string, error) {
	if len(data) != len(paths) {
		return nil, ErrTrainingCapabilitiesUnverified
	}
	capabilities := make(map[string][]string, len(paths))
	for _, path := range paths {
		array, ok := data[path].([]any)
		if !ok || len(array) > 9 {
			return nil, ErrTrainingCapabilitiesUnverified
		}
		values := make([]string, 0, len(array))
		for _, val := range array {
			capability, ok := val.(string)
			if !ok {
				return nil, ErrTrainingCapabilitiesUnverified
			}
			values = append(values, capability)
		}
		capabilities[path] = values
	}
	return validateTrainingCapabilities(paths, capabilities)
}

func uniqueTrainingJSON(decoder *json.Decoder, depth int) (any, error) {
	if depth > 4 {
		return nil, ErrTrainingCapabilitiesUnverified
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := map[string]any{}
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := token.(string)
			if !ok {
				return nil, ErrTrainingCapabilitiesUnverified
			}
			if _, duplicate := object[key]; duplicate {
				return nil, ErrTrainingCapabilitiesUnverified
			}
			value, err := uniqueTrainingJSON(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
			return nil, ErrTrainingCapabilitiesUnverified
		}
		return object, nil
	case json.Delim('['):
		array := []any{}
		for decoder.More() {
			value, err := uniqueTrainingJSON(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
			return nil, ErrTrainingCapabilitiesUnverified
		}
		return array, nil
	default:
		if _, delimiter := token.(json.Delim); delimiter {
			return nil, ErrTrainingCapabilitiesUnverified
		}
		return token, nil
	}
}
