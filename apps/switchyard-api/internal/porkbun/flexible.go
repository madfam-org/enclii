package porkbun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// Tolerant scalar types for Porkbun's response payloads.
//
// Porkbun does not version its JSON API, and it has silently changed the
// wire type of its boolean-ish domain flags: fields that were emitted as
// JSON numbers (1/0) are now emitted as JSON strings ("1"/"0"). On
// 2026-08-21 this broke `enclii providers porkbun domains --json` outright
// with:
//
//	decode porkbun response: json: cannot unmarshal string into Go struct
//	field Domain.domains.securityLock of type int
//
// while provisioning the kalya.app apex. Pinning the struct to whichever
// shape happens to be live today just re-arms the same failure for the next
// flip, so every field a registrar could plausibly stringify decodes through
// these types instead. They accept a JSON number, a numeric string, a
// bare boolean, and null, and they marshal back out in the original
// canonical shape (number for FlexibleInt, boolean for FlexibleBool) so
// every downstream JSON consumer — the operator read adapters and the CLI
// --json output they feed — sees exactly what it saw before.

// FlexibleInt is an int that decodes from a JSON number, a numeric string,
// or a boolean, and always encodes as a JSON number.
type FlexibleInt int

// Int returns the underlying int value.
func (f FlexibleInt) Int() int { return int(f) }

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexibleInt) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*f = 0
		return nil
	}

	// Boolean form: some Porkbun-adjacent payloads use true/false for the
	// same flags. Map onto the 1/0 the int form has always used.
	if bytes.Equal(data, []byte("true")) {
		*f = 1
		return nil
	}
	if bytes.Equal(data, []byte("false")) {
		*f = 0
		return nil
	}

	// String form ("1" / "0" / ""), which is what Porkbun returns today.
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		return f.parseString(s)
	}

	// Number form (1 / 0), the historical shape. Decode via float64 so a
	// value that arrives as 1.0 does not hard-fail.
	var n float64
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("porkbun: cannot decode %s as integer flag: %w", string(data), err)
	}
	*f = FlexibleInt(int(n))
	return nil
}

func (f *FlexibleInt) parseString(s string) error {
	trimmed := trimSpace(s)
	switch trimmed {
	case "":
		*f = 0
		return nil
	case "true", "TRUE", "True", "yes", "YES", "Yes":
		*f = 1
		return nil
	case "false", "FALSE", "False", "no", "NO", "No":
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(trimmed)
	if err != nil {
		// Fall back to a float parse so "1.0" behaves like the number form.
		fl, ferr := strconv.ParseFloat(trimmed, 64)
		if ferr != nil {
			return fmt.Errorf("porkbun: cannot decode %q as integer flag: %w", s, err)
		}
		*f = FlexibleInt(int(fl))
		return nil
	}
	*f = FlexibleInt(n)
	return nil
}

// MarshalJSON implements json.Marshaler, emitting the canonical number form
// so downstream consumers see the pre-drift shape regardless of what
// Porkbun sent.
func (f FlexibleInt) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Itoa(int(f))), nil
}

// FlexibleString is a string that decodes from a JSON string or a JSON
// number, and always encodes as a JSON string. Porkbun already returns
// numeric-looking DNS fields (ttl, prio, id) as strings; this keeps them
// decoding if the registrar flips them to real numbers.
type FlexibleString string

// String returns the underlying string value.
func (f FlexibleString) String() string { return string(f) }

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexibleString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*f = ""
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = FlexibleString(s)
		return nil
	}
	if bytes.Equal(data, []byte("true")) || bytes.Equal(data, []byte("false")) {
		*f = FlexibleString(string(data))
		return nil
	}
	// Number form: preserve the literal so an integer TTL round-trips as
	// "600" rather than "600.000000".
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("porkbun: cannot decode %s as string field: %w", string(data), err)
	}
	*f = FlexibleString(n.String())
	return nil
}

// MarshalJSON implements json.Marshaler, emitting the canonical string form.
func (f FlexibleString) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(f))
}

// trimSpace trims ASCII whitespace without pulling in strings for this hot
// decode path's single use.
func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && isSpace(s[start]) {
		start++
	}
	for end > start && isSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
}
