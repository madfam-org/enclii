package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const karafielJanuaIssuer = "https://auth.madfam.io"

// A bounded read-only transport, with a fixed origin and no redirects. The
// admin credential is sent only to the reviewed /oauth/clients metadata API.
func readKarafielJanuaJSON(ctx context.Context, client *http.Client, path, token string, out any) error {
	if client == nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\r\n\x00#") {
		return errKarafielStage
	}
	if token != "" && !strings.HasPrefix(path, "/api/v1/oauth/clients/") {
		return errKarafielStage
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, karafielJanuaIssuer+path, nil)
	if err != nil {
		return errKarafielStage
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	request.Header.Set("Accept", "application/json")
	bounded := *client
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := bounded.Do(request)
	if err != nil {
		return errKarafielStage
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errKarafielStage
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(raw) > 1024*1024 {
		return errKarafielStage
	}
	var projection map[string]interface{}
	if decodeKarafielProviderJSON(raw, &projection) != nil || karafielProviderCredentials(projection) {
		return errKarafielStage
	}
	// Unknown non-secret provider metadata is ignored only after bounded UTF-8,
	// one-value and duplicate-key validation over the COMPLETE response.
	if json.Unmarshal(raw, out) != nil {
		return errKarafielStage
	}
	return nil
}

// Provider inventories are larger than closed native receipts. Keep their
// separate 1 MiB ceiling, bounded nesting, UTF-8, one-object, and duplicate-key
// validation here; never relax the native 8 KiB intake/receipt parser.
func decodeKarafielProviderJSON(raw []byte, out *map[string]interface{}) error {
	if len(raw) == 0 || len(raw) > 1024*1024 || !utf8.Valid(raw) {
		return errKarafielStage
	}
	clean := bytes.TrimSpace(raw)
	if len(clean) == 0 || clean[0] != '{' {
		return errKarafielStage
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 64 {
			return errKarafielStage
		}
		token, err := decoder.Token()
		if err != nil {
			return errKarafielStage
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				token, err := decoder.Token()
				key, ok := token.(string)
				if err != nil || !ok || seen[key] {
					return errKarafielStage
				}
				seen[key] = true
				if walk(depth+1) != nil {
					return errKarafielStage
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return errKarafielStage
			}
		case '[':
			for decoder.More() {
				if walk(depth+1) != nil {
					return errKarafielStage
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return errKarafielStage
			}
		default:
			return errKarafielStage
		}
		return nil
	}
	if walk(0) != nil {
		return errKarafielStage
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errKarafielStage
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(out) != nil {
		return errKarafielStage
	}
	return nil
}

func karafielProviderCredentials(value interface{}) bool {
	switch row := value.(type) {
	case map[string]interface{}:
		for key, v := range row {
			lower := strings.ToLower(key)
			if (lower == "client_secret" || lower == "access_token" || lower == "refresh_token" || lower == "id_token" || lower == "password" || lower == "private_key") && v != nil && v != "" {
				return true
			}
			if karafielProviderCredentials(v) {
				return true
			}
		}
	case []interface{}:
		for _, v := range row {
			if karafielProviderCredentials(v) {
				return true
			}
		}
	}
	return false
}

type karafielJanuaClient struct {
	ID             string   `json:"id"`
	ClientID       string   `json:"client_id"`
	ClientSecret   *string  `json:"client_secret"`
	Name           string   `json:"name"`
	Audience       *string  `json:"audience"`
	ClientKey      *string  `json:"client_key,omitempty"`
	RedirectURIs   []string `json:"redirect_uris"`
	OrganizationID *string  `json:"organization_id"`
	AllowedScopes  []string `json:"allowed_scopes"`
	GrantTypes     []string `json:"grant_types"`
	IsConfidential bool     `json:"is_confidential"`
	IsActive       bool     `json:"is_active"`
}

func exactKarafielStrings(actual, wanted []string) bool {
	if len(actual) != len(wanted) {
		return false
	}
	a, b := append([]string{}, actual...), append([]string{}, wanted...)
	sort.Strings(a)
	sort.Strings(b)
	return reflect.DeepEqual(a, b)
}

func qualifiedKarafielJanuaClient(client karafielJanuaClient, role, id string) bool {
	if role != "web" && role != "admin" {
		return false
	}
	internal, e := uuid.Parse(client.ID)
	if e != nil || internal == uuid.Nil || internal.String() != client.ID {
		return false
	}
	name := "Karafiel Staging Web"
	origin := "https://staging-app.karafiel.mx"
	if role == "admin" {
		name = "Karafiel Staging Admin"
		origin = "https://staging-admin.karafiel.mx"
	}
	// Janua may expose client_key as an audience alias. Identity comes from the
	// selected internal/public IDs and exact name, never that shared alias.
	keyOK := client.ClientKey == nil || *client.ClientKey == "karafiel-staging-"+role || *client.ClientKey == "karafiel-staging-api"
	return client.ClientID == id && id != "" && client.Name == name && client.ClientSecret == nil && client.IsConfidential && client.IsActive && client.Audience != nil && *client.Audience == "karafiel-staging-api" && keyOK && (client.OrganizationID == nil || *client.OrganizationID == "") && exactKarafielStrings(client.RedirectURIs, []string{origin + "/api/auth/callback"}) && exactKarafielStrings(client.AllowedScopes, []string{"openid", "profile", "email", "offline_access"}) && exactKarafielStrings(client.GrantTypes, []string{"authorization_code", "refresh_token"})
}

func readKarafielJanuaRegistration(ctx context.Context, client *http.Client, token, role, id string) (string, error) {
	if strings.TrimSpace(token) == "" || id == "" || len(id) > 256 || strings.ContainsAny(id, "\r\n\x00") {
		return "", errKarafielStage
	}
	var selected *karafielJanuaClient
	count, total := 0, -1
	for page := 1; page <= 20; page++ {
		var result struct {
			Clients []karafielJanuaClient `json:"clients"`
			Total   int                   `json:"total"`
		}
		if readKarafielJanuaJSON(ctx, client, "/api/v1/oauth/clients/admin/all?page="+big.NewInt(int64(page)).String()+"&per_page=100", token, &result) != nil || len(result.Clients) > 100 || result.Total < 0 || result.Total > 2000 {
			return "", errKarafielStage
		}
		if total < 0 {
			total = result.Total
		} else if total != result.Total {
			return "", errKarafielStage
		}
		for _, candidate := range result.Clients {
			if candidate.ClientSecret != nil {
				return "", errKarafielStage
			}
			if candidate.ClientID == id || candidate.Name == "Karafiel Staging "+map[string]string{"web": "Web", "admin": "Admin"}[role] {
				if selected != nil || candidate.ClientID != id {
					return "", errKarafielStage
				}
				copy := candidate
				selected = &copy
			}
		}
		count += len(result.Clients)
		if count == total {
			break
		}
		if count > total || len(result.Clients) == 0 || page == 20 {
			return "", errKarafielStage
		}
	}
	if selected == nil {
		return "", errKarafielStage
	}
	var detail karafielJanuaClient
	internal, err := uuid.Parse(selected.ID)
	if err != nil || internal == uuid.Nil || internal.String() != selected.ID {
		return "", errKarafielStage
	}
	if readKarafielJanuaJSON(ctx, client, "/api/v1/oauth/clients/"+selected.ID, token, &detail) != nil || !qualifiedKarafielJanuaClient(detail, role, id) || detail.ID != selected.ID || detail.ClientID != selected.ClientID || detail.Name != selected.Name {
		return "", errKarafielStage
	}
	return stageJSONHash(detail), nil
}

func readKarafielJanuaIssuer(ctx context.Context, client *http.Client) (string, error) {
	var discovery map[string]interface{}
	if readKarafielJanuaJSON(ctx, client, "/.well-known/openid-configuration", "", &discovery) != nil || discovery["issuer"] != karafielJanuaIssuer || discovery["jwks_uri"] != karafielJanuaIssuer+"/.well-known/jwks.json" {
		return "", errKarafielStage
	}
	for _, key := range []string{"authorization_endpoint", "token_endpoint"} {
		raw, ok := discovery[key].(string)
		if !ok {
			return "", errKarafielStage
		}
		parsed, e := url.Parse(raw)
		if e != nil || parsed.Scheme != "https" || parsed.Host != "auth.madfam.io" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.HasPrefix(parsed.Path, "/api/v1/oauth/") {
			return "", errKarafielStage
		}
	}
	var jwks struct {
		Keys []map[string]interface{} `json:"keys"`
	}
	if readKarafielJanuaJSON(ctx, client, "/.well-known/jwks.json", "", &jwks) != nil || len(jwks.Keys) == 0 || len(jwks.Keys) > 32 {
		return "", errKarafielStage
	}
	seen := map[string]bool{}
	for _, key := range jwks.Keys {
		kid, ok := key["kid"].(string)
		if !ok || kid == "" || len(kid) > 256 || seen[kid] || key["kty"] != "RSA" || key["use"] != "sig" || key["alg"] != "RS256" {
			return "", errKarafielStage
		}
		seen[kid] = true
		for _, field := range []string{"d", "p", "q", "dp", "dq", "qi", "oth", "k"} {
			if _, found := key[field]; found {
				return "", errKarafielStage
			}
		}
		modulus, nok := key["n"].(string)
		exponent, eok := key["e"].(string)
		n, ne := base64.RawURLEncoding.DecodeString(modulus)
		e, ee := base64.RawURLEncoding.DecodeString(exponent)
		if !nok || !eok || ne != nil || ee != nil || new(big.Int).SetBytes(n).BitLen() < 2048 || len(n) > 1024 || len(e) == 0 || len(e) > 4 {
			return "", errKarafielStage
		}
		power := new(big.Int).SetBytes(e)
		if !power.IsInt64() || power.Int64() < 3 || power.Bit(0) == 0 {
			return "", errKarafielStage
		}
	}
	return stageJSONHash(struct {
		Discovery map[string]interface{}
		Keys      []map[string]interface{}
	}{discovery, jwks.Keys}), nil
}
