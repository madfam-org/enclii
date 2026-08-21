package porkbun

import (
	"encoding/json"
	"testing"
)

// Regression coverage for the Porkbun response decode drift hit live on
// 2026-08-21 while provisioning the kalya.app apex:
//
//	enclii providers porkbun domains --json
//	  -> decode porkbun response: json: cannot unmarshal string into Go
//	     struct field Domain.domains.securityLock of type int
//
// Every case below is asserted in BOTH directions — the historical numeric
// shape and today's string shape — so a future flip back cannot break the
// client either.

func TestFlexibleInt_UnmarshalBothShapes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  FlexibleInt
	}{
		// Historical numeric shape.
		{"number one", `1`, 1},
		{"number zero", `0`, 0},
		{"number other", `42`, 42},
		{"number float", `1.0`, 1},
		// Today's string shape — the shape that broke prod.
		{"string one", `"1"`, 1},
		{"string zero", `"0"`, 0},
		{"string other", `"42"`, 42},
		{"string padded", `" 1 "`, 1},
		{"string empty", `""`, 0},
		{"string float", `"1.0"`, 1},
		// Boolean shapes, accepted defensively.
		{"bool true", `true`, 1},
		{"bool false", `false`, 0},
		{"string true", `"true"`, 1},
		{"string false", `"false"`, 0},
		{"string yes", `"yes"`, 1},
		{"string no", `"no"`, 0},
		// Absent value.
		{"null", `null`, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got FlexibleInt
			if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
				t.Fatalf("Unmarshal(%s) returned error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("Unmarshal(%s) = %d, want %d", tt.input, got, tt.want)
			}
			if got.Int() != int(tt.want) {
				t.Errorf("Int() = %d, want %d", got.Int(), int(tt.want))
			}
		})
	}
}

func TestFlexibleInt_UnmarshalRejectsGarbage(t *testing.T) {
	for _, input := range []string{`"not-a-number"`, `{}`, `[]`} {
		var got FlexibleInt
		if err := json.Unmarshal([]byte(input), &got); err == nil {
			t.Errorf("Unmarshal(%s) unexpectedly succeeded with %d", input, got)
		}
	}
}

// FlexibleInt must still marshal as a bare JSON number so the operator read
// adapters (which put domain.AutoRenew straight into their gin.H payload)
// and the CLI --json output keep the exact shape they had before the fix.
func TestFlexibleInt_MarshalsAsNumber(t *testing.T) {
	payload, err := json.Marshal(struct {
		AutoRenew FlexibleInt `json:"autoRenew"`
	}{AutoRenew: 1})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if string(payload) != `{"autoRenew":1}` {
		t.Errorf("Marshal = %s, want {\"autoRenew\":1}", payload)
	}
}

func TestFlexibleString_UnmarshalBothShapes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  FlexibleString
	}{
		{"string", `"600"`, "600"},
		{"string empty", `""`, ""},
		{"string text", `"ACTIVE"`, "ACTIVE"},
		{"number int", `600`, "600"},
		{"number zero", `0`, "0"},
		{"bool", `true`, "true"},
		{"null", `null`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got FlexibleString
			if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
				t.Fatalf("Unmarshal(%s) returned error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("Unmarshal(%s) = %q, want %q", tt.input, got, tt.want)
			}
			if got.String() != string(tt.want) {
				t.Errorf("String() = %q, want %q", got.String(), string(tt.want))
			}
		})
	}
}

func TestFlexibleString_MarshalsAsString(t *testing.T) {
	payload, err := json.Marshal(struct {
		TTL FlexibleString `json:"ttl"`
	}{TTL: "600"})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if string(payload) != `{"ttl":"600"}` {
		t.Errorf("Marshal = %s, want {\"ttl\":\"600\"}", payload)
	}
}

// listDomainsNumericFixture is the shape Porkbun returned historically, and
// which the int-typed struct was written against.
const listDomainsNumericFixture = `{
  "status": "SUCCESS",
  "domains": [
    {
      "domain": "kalya.app",
      "status": "ACTIVE",
      "tld": "app",
      "createDate": "2026-08-21 10:00:00",
      "expireDate": "2027-08-21 10:00:00",
      "securityLock": 1,
      "whoisPrivacy": 1,
      "autoRenew": 0,
      "apiAccess": 1,
      "notLocal": 0
    }
  ]
}`

// listDomainsStringFixture is the shape Porkbun returns today — identical
// values, every flag stringified. This is the payload that produced the
// production decode failure.
const listDomainsStringFixture = `{
  "status": "SUCCESS",
  "domains": [
    {
      "domain": "kalya.app",
      "status": "ACTIVE",
      "tld": "app",
      "createDate": "2026-08-21 10:00:00",
      "expireDate": "2027-08-21 10:00:00",
      "securityLock": "1",
      "whoisPrivacy": "1",
      "autoRenew": "0",
      "apiAccess": "1",
      "notLocal": "0"
    }
  ]
}`

func TestListDomainsResponse_DecodesBothShapesIdentically(t *testing.T) {
	fixtures := map[string]string{
		"numeric (historical)": listDomainsNumericFixture,
		"string (current)":     listDomainsStringFixture,
	}

	for name, fixture := range fixtures {
		t.Run(name, func(t *testing.T) {
			var out ListDomainsResponse
			if err := json.Unmarshal([]byte(fixture), &out); err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			if out.Status.String() != "SUCCESS" {
				t.Errorf("Status = %q, want SUCCESS", out.Status)
			}
			if len(out.Domains) != 1 {
				t.Fatalf("got %d domains, want 1", len(out.Domains))
			}
			d := out.Domains[0]
			if d.Domain.String() != "kalya.app" {
				t.Errorf("Domain = %q, want kalya.app", d.Domain)
			}
			if d.Status.String() != "ACTIVE" {
				t.Errorf("Status = %q, want ACTIVE", d.Status)
			}
			if d.TLD.String() != "app" {
				t.Errorf("TLD = %q, want app", d.TLD)
			}
			if d.ExpireDate.String() != "2027-08-21 10:00:00" {
				t.Errorf("ExpireDate = %q", d.ExpireDate)
			}
			if d.SecurityLock.Int() != 1 {
				t.Errorf("SecurityLock = %d, want 1", d.SecurityLock)
			}
			if d.WhoisPrivacy.Int() != 1 {
				t.Errorf("WhoisPrivacy = %d, want 1", d.WhoisPrivacy)
			}
			if d.AutoRenew.Int() != 0 {
				t.Errorf("AutoRenew = %d, want 0", d.AutoRenew)
			}
			if d.APIAccess.Int() != 1 {
				t.Errorf("APIAccess = %d, want 1", d.APIAccess)
			}
			if d.NotLocal.Int() != 0 {
				t.Errorf("NotLocal = %d, want 0", d.NotLocal)
			}
		})
	}
}

// Both wire shapes must re-marshal to the same canonical JSON, which is what
// guarantees the operator read adapters emit an unchanged payload no matter
// which shape Porkbun sent.
func TestListDomainsResponse_RemarshalsToCanonicalShape(t *testing.T) {
	var fromNumeric, fromString ListDomainsResponse
	if err := json.Unmarshal([]byte(listDomainsNumericFixture), &fromNumeric); err != nil {
		t.Fatalf("decode numeric fixture: %v", err)
	}
	if err := json.Unmarshal([]byte(listDomainsStringFixture), &fromString); err != nil {
		t.Fatalf("decode string fixture: %v", err)
	}

	numericJSON, err := json.Marshal(fromNumeric)
	if err != nil {
		t.Fatalf("marshal numeric: %v", err)
	}
	stringJSON, err := json.Marshal(fromString)
	if err != nil {
		t.Fatalf("marshal string: %v", err)
	}
	if string(numericJSON) != string(stringJSON) {
		t.Errorf("shapes diverge after round-trip:\n numeric: %s\n string:  %s", numericJSON, stringJSON)
	}

	// Flags must come back out as numbers, exactly as the pre-drift struct
	// emitted them.
	var canonical struct {
		Domains []struct {
			Domain       string `json:"domain"`
			SecurityLock int    `json:"securityLock"`
			AutoRenew    int    `json:"autoRenew"`
		} `json:"domains"`
	}
	if err := json.Unmarshal(stringJSON, &canonical); err != nil {
		t.Fatalf("canonical output is not int-shaped: %v", err)
	}
	if len(canonical.Domains) != 1 || canonical.Domains[0].SecurityLock != 1 || canonical.Domains[0].AutoRenew != 0 {
		t.Errorf("canonical output mismatch: %+v", canonical.Domains)
	}
	if canonical.Domains[0].Domain != "kalya.app" {
		t.Errorf("canonical domain = %q", canonical.Domains[0].Domain)
	}
}

func TestGetDomainResponse_DecodesBothShapes(t *testing.T) {
	numeric := `{"status":"SUCCESS","domain":{"domain":"kalya.app","securityLock":1,"autoRenew":1}}`
	stringy := `{"status":"SUCCESS","domain":{"domain":"kalya.app","securityLock":"1","autoRenew":"1"}}`

	for name, fixture := range map[string]string{"numeric": numeric, "string": stringy} {
		t.Run(name, func(t *testing.T) {
			var out GetDomainResponse
			if err := json.Unmarshal([]byte(fixture), &out); err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			if out.Domain.SecurityLock.Int() != 1 || out.Domain.AutoRenew.Int() != 1 {
				t.Errorf("flags = %d/%d, want 1/1", out.Domain.SecurityLock, out.Domain.AutoRenew)
			}
		})
	}
}

func TestDNSRecordsResponse_DecodesStringAndNumericScalars(t *testing.T) {
	// ttl/prio/id arrive as strings today; a flip to numbers must not break
	// decoding the way securityLock did.
	stringy := `{"status":"SUCCESS","records":[{"id":"123","name":"app.kalya.app","type":"CNAME","content":"x.cfargotunnel.com","ttl":"600","prio":"0","notes":""}]}`
	numeric := `{"status":"SUCCESS","records":[{"id":123,"name":"app.kalya.app","type":"CNAME","content":"x.cfargotunnel.com","ttl":600,"prio":0,"notes":""}]}`

	for name, fixture := range map[string]string{"string": stringy, "numeric": numeric} {
		t.Run(name, func(t *testing.T) {
			var out DNSRecordsResponse
			if err := json.Unmarshal([]byte(fixture), &out); err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			if len(out.Records) != 1 {
				t.Fatalf("got %d records, want 1", len(out.Records))
			}
			r := out.Records[0]
			if r.ID.String() != "123" {
				t.Errorf("ID = %q, want 123", r.ID)
			}
			if r.TTL.String() != "600" {
				t.Errorf("TTL = %q, want 600", r.TTL)
			}
			if r.Prio.String() != "0" {
				t.Errorf("Prio = %q, want 0", r.Prio)
			}
			if r.Type.String() != "CNAME" {
				t.Errorf("Type = %q, want CNAME", r.Type)
			}
		})
	}
}

// responseFailed reads the envelope through the flexible types; an error
// envelope whose code arrives as a number must still surface the message.
func TestResponseFailed_HandlesBothCodeShapes(t *testing.T) {
	for name, fixture := range map[string]string{
		"string code":  `{"status":"ERROR","message":"Invalid domain","code":"400"}`,
		"numeric code": `{"status":"ERROR","message":"Invalid domain","code":400}`,
	} {
		t.Run(name, func(t *testing.T) {
			var out BasicResponse
			if err := json.Unmarshal([]byte(fixture), &out); err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			failed, message := responseFailed(&out)
			if !failed {
				t.Fatal("expected responseFailed to report failure")
			}
			if message != "400: Invalid domain" {
				t.Errorf("message = %q, want %q", message, "400: Invalid domain")
			}
		})
	}
}

func TestResponseFailed_SuccessEnvelope(t *testing.T) {
	var out BasicResponse
	if err := json.Unmarshal([]byte(`{"status":"SUCCESS"}`), &out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if failed, message := responseFailed(&out); failed {
		t.Errorf("expected success, got failure: %q", message)
	}
}
