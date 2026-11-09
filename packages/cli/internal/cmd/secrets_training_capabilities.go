package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/spf13/cobra"
)

const trainingCapabilitiesRoute = "/v1/admin/secrets/training-custody-capabilities"
const trainingCapabilitiesVersion = "training-custody-capabilities-v1"
const trainingCapabilitiesMaxBody = 64 * 1024

type trainingCapabilitiesPath struct {
	Path         string   `json:"path"`
	Capabilities []string `json:"capabilities"`
	Required     []string `json:"required"`
	Missing      []string `json:"missing_required"`
	Outside      []string `json:"outside_expected"`
	Unknown      *bool    `json:"unknown"`
}

type trainingCapabilitiesReceipt struct {
	Version    string                     `json:"version"`
	ObservedAt string                     `json:"observed_at"`
	Status     string                     `json:"status"`
	AllWithin  *bool                      `json:"all_required_within_expected_scope"`
	Paths      []trainingCapabilitiesPath `json:"paths"`
	Limits     []string                   `json:"limits"`
}

func trainingCapabilitiesLimits() []string {
	return []string{"Capabilities of the current scoped writer on ten fixed registry-derived paths only.", "This is not proof of delivered values, credential ownership, namespace/admission/readiness, or permission to mint credentials.", "No KV values, policy content or administrative credentials were read or changed; no automatic retry.", "Capabilities are path authorization evidence; parameter constraints and actual intake delivery remain separate checks."}
}

func trainingCapabilitiesPaths() []string {
	paths := make([]string, 0, 10)
	for _, name := range []string{"crea-sim-gateway", "crea-sim-captura", "crea-sim-map01", "crea-sim-selva", "janua-staging"} {
		paths = append(paths, "secret/data/"+name, "secret/metadata/"+name)
	}
	sort.Strings(paths)
	return paths
}

func newSecretsTrainingCapabilitiesCommand(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	command := &cobra.Command{Use: "training-capabilities", Short: "Inspect current writer capabilities on ten fixed training custody paths", Long: "Perform one fixed operator GET of path authorization metadata. No values, credentials, policy content or administrative custody are read. This observation does not authorize credential minting or prove delivery, ownership, namespace/admission or runtime readiness.", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			bad := fmt.Errorf("training capability observation unavailable; upstream details withheld")
			if cfg == nil {
				return bad
			}
			token := cfg.APIToken
			if token == "" && cfg.Credentials != nil {
				token = cfg.Credentials.AccessToken
			}
			if strings.TrimSpace(token) == "" {
				return fmt.Errorf("sign in through enclii login with an existing platform-operator account")
			}
			base, err := url.Parse(cfg.APIEndpoint)
			if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || (base.Path != "" && base.Path != "/") || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" {
				return bad
			}
			request, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, strings.TrimSuffix(base.String(), "/")+trainingCapabilitiesRoute, nil)
			if err != nil {
				return bad
			}
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("User-Agent", "enclii-cli-training-capabilities/1.0")
			// A fresh HTTP/1 connection cannot use Go's reused-connection or
			// HTTP/2 replay paths. There is no shared cookie jar or retry loop.
			protocols := new(http.Protocols)
			protocols.SetHTTP1(true)
			transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DisableKeepAlives: true, Protocols: protocols, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 12 * time.Second}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			response, err := client.Do(request)
			if err != nil {
				return bad
			}
			defer func() { _ = response.Body.Close() }()
			if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusBadGateway && response.StatusCode != http.StatusServiceUnavailable {
				return bad
			}
			body, err := io.ReadAll(io.LimitReader(response.Body, trainingCapabilitiesMaxBody+1))
			if err != nil || len(body) > trainingCapabilitiesMaxBody {
				return bad
			}
			receipt, err := decodeTrainingCapabilities(body, response.StatusCode)
			if err != nil {
				return bad
			}
			if jsonOut {
				if json.NewEncoder(cmd.OutOrStdout()).Encode(receipt) != nil {
					return fmt.Errorf("safe training capability output unavailable")
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "status: %s\nobserved_at: %s\nall_required_within_expected_scope: %t\n", receipt.Status, receipt.ObservedAt, *receipt.AllWithin)
				for _, path := range receipt.Paths {
					fmt.Fprintf(cmd.OutOrStdout(), "%s capabilities=%s missing=%s outside=%s unknown=%t\n", path.Path, strings.Join(path.Capabilities, ","), strings.Join(path.Missing, ","), strings.Join(path.Outside, ","), *path.Unknown)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Path authorization observation only; no credential mint, delivery, ownership or readiness authority.")
			}
			if receipt.Status != "verified_required_capabilities" {
				return fmt.Errorf("training custody observation is %s; required scoped capabilities are not verified", receipt.Status)
			}
			return nil
		}}
	command.Flags().BoolVar(&jsonOut, "json", false, "Print only validated fixed-path capability metadata")
	return command
}

func decodeTrainingCapabilities(body []byte, status int) (trainingCapabilitiesReceipt, error) {
	bad := fmt.Errorf("training capability receipt refused")
	var receipt trainingCapabilitiesReceipt
	if len(body) == 0 || len(body) > trainingCapabilitiesMaxBody || !utf8.Valid(body) || (status != http.StatusOK && status != http.StatusBadGateway && status != http.StatusServiceUnavailable) {
		return receipt, bad
	}
	unique := json.NewDecoder(bytes.NewReader(body))
	if trainingCapabilitiesUniqueJSON(unique, 0) != nil {
		return receipt, bad
	}
	if _, err := unique.Token(); err != io.EOF {
		return receipt, bad
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&receipt) != nil || receipt.Version != trainingCapabilitiesVersion || receipt.AllWithin == nil || !reflect.DeepEqual(receipt.Limits, trainingCapabilitiesLimits()) {
		return receipt, bad
	}
	observed, err := time.Parse(time.RFC3339Nano, receipt.ObservedAt)
	if err != nil || observed.IsZero() || observed.UTC().Format(time.RFC3339Nano) != receipt.ObservedAt {
		return receipt, bad
	}
	paths := trainingCapabilitiesPaths()
	if len(receipt.Paths) != len(paths) {
		return receipt, bad
	}
	sort.Slice(receipt.Paths, func(i, j int) bool { return receipt.Paths[i].Path < receipt.Paths[j].Path })
	derived, within := "verified_required_capabilities", true
	unknownReceipt := status == http.StatusBadGateway || status == http.StatusServiceUnavailable
	for i := range receipt.Paths {
		path := &receipt.Paths[i]
		if path.Path != paths[i] || path.Unknown == nil || *path.Unknown != unknownReceipt || path.Capabilities == nil || path.Required == nil || path.Missing == nil || path.Outside == nil {
			return receipt, bad
		}
		required := []string{"create", "patch", "read", "update"}
		if strings.HasPrefix(path.Path, "secret/metadata/") {
			required = []string{"list", "read"}
		}
		for _, values := range [][]string{path.Capabilities, path.Required, path.Missing, path.Outside} {
			if !trainingCapabilitiesSet(values) {
				return receipt, bad
			}
		}
		if !reflect.DeepEqual(path.Required, required) {
			return receipt, bad
		}
		if unknownReceipt {
			if len(path.Capabilities) != 0 || len(path.Missing) != 0 || len(path.Outside) != 0 {
				return receipt, bad
			}
			continue
		}
		if len(path.Capabilities) == 0 {
			return receipt, bad
		}
		actual, expected := map[string]bool{}, map[string]bool{}
		for _, value := range path.Capabilities {
			actual[value] = true
		}
		if actual["deny"] && len(actual) != 1 {
			return receipt, bad
		}
		missing, outside := []string{}, []string{}
		for _, value := range required {
			expected[value] = true
			if !actual[value] {
				missing = append(missing, value)
			}
		}
		for _, value := range path.Capabilities {
			if value != "deny" && !expected[value] {
				outside = append(outside, value)
			}
		}
		if !reflect.DeepEqual(missing, path.Missing) || !reflect.DeepEqual(outside, path.Outside) {
			return receipt, bad
		}
		if len(missing) > 0 {
			within = false
			if derived != "outside_expected_scoped_contract" {
				derived = "missing_required"
			}
		}
		if len(outside) > 0 {
			within = false
			derived = "outside_expected_scoped_contract"
		}
	}
	if unknownReceipt {
		within = false
		derived = "unverified"
		if status == http.StatusServiceUnavailable {
			derived = "unavailable"
		}
	}
	if receipt.Status != derived || *receipt.AllWithin != within {
		return receipt, bad
	}
	receipt.Limits = trainingCapabilitiesLimits()
	return receipt, nil
}

func trainingCapabilitiesSet(values []string) bool {
	if len(values) > 9 {
		return false
	}
	allowed := map[string]bool{"create": true, "read": true, "update": true, "patch": true, "delete": true, "list": true, "sudo": true, "root": true, "deny": true}
	seen := map[string]bool{}
	for _, value := range values {
		if !allowed[value] || seen[value] {
			return false
		}
		seen[value] = true
	}
	sort.Strings(values)
	return true
}

func trainingCapabilitiesUniqueJSON(decoder *json.Decoder, depth int) error {
	bad := fmt.Errorf("ambiguous capability JSON refused")
	if depth > 16 {
		return bad
	}
	token, err := decoder.Token()
	if err != nil {
		return bad
	}
	if delimiter, ok := token.(json.Delim); ok {
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || name != strings.ToLower(name) || seen[name] {
					return bad
				}
				seen[name] = true
				if trainingCapabilitiesUniqueJSON(decoder, depth+1) != nil {
					return bad
				}
			}
			if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
				return bad
			}
		case '[':
			for decoder.More() {
				if trainingCapabilitiesUniqueJSON(decoder, depth+1) != nil {
					return bad
				}
			}
			if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
				return bad
			}
		default:
			return bad
		}
	}
	return nil
}
