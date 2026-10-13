package workloadtargets

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/util/validation"
)

type Request struct {
	Project, Environment, Namespace, Deployment string
	ServiceID                                   uuid.UUID
	DryRun                                      bool
	ExpectedPlan, Reason                        string
	IdempotencyID                               uuid.UUID
}

// ParseRequest is closed, bounded and duplicate-key rejecting. Apply is parsed
// requires a reviewed hash, a bounded reason and a canonical UUID for apply.
func ParseRequest(data []byte) (Request, error) {
	if len(data) == 0 || len(data) > 8192 {
		return Request{}, ErrIntent
	}
	unique := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueValue(unique, 0); err != nil {
		return Request{}, err
	}
	if _, err := unique.Token(); err != io.EOF {
		return Request{}, ErrIntent
	}
	var wire struct {
		Operation      string            `json:"operation"`
		DryRun         *bool             `json:"dry_run"`
		ExpectedPlan   string            `json:"expect_plan_sha256"`
		Reason         string            `json:"reason"`
		IdempotencyKey string            `json:"idempotency_key"`
		Scope          map[string]string `json:"scope"`
		Args           map[string]string `json:"args"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&wire) != nil || wire.DryRun == nil || wire.Operation != "" && wire.Operation != "ops.apps.bind-target" {
		return Request{}, ErrIntent
	}
	if len(wire.Scope) != 3 || wire.Scope["project"] == "" || wire.Scope["environment"] == "" || wire.Scope["service"] == "" {
		return Request{}, ErrIntent
	}
	if len(wire.Args) != 2 || wire.Args["namespace"] == "" || wire.Args["deployment"] == "" {
		return Request{}, ErrIntent
	}
	service, err := uuid.Parse(wire.Scope["service"])
	if err != nil || service == uuid.Nil || service.String() != wire.Scope["service"] {
		return Request{}, ErrIntent
	}
	for _, value := range []string{wire.Scope["project"], wire.Scope["environment"], wire.Args["namespace"]} {
		if len(validation.IsDNS1123Label(value)) != 0 {
			return Request{}, ErrIntent
		}
	}
	if len(validation.IsDNS1123Subdomain(wire.Args["deployment"])) != 0 {
		return Request{}, ErrIntent
	}
	var id uuid.UUID
	if !*wire.DryRun {
		id, err = uuid.Parse(wire.IdempotencyKey)
		if err != nil || id == uuid.Nil || id.String() != wire.IdempotencyKey || !sha64.MatchString(wire.ExpectedPlan) ||
			strings.TrimSpace(wire.Reason) == "" || len(wire.Reason) > 1000 || strings.IndexFunc(wire.Reason, unicode.IsControl) >= 0 {
			return Request{}, ErrIntent
		}
	} else if wire.ExpectedPlan != "" || wire.IdempotencyKey != "" || wire.Reason != "" {
		return Request{}, ErrIntent
	}
	return Request{ExpectedPlan: wire.ExpectedPlan, Reason: wire.Reason, IdempotencyID: id, Project: wire.Scope["project"], Environment: wire.Scope["environment"], Namespace: wire.Args["namespace"], Deployment: wire.Args["deployment"], ServiceID: service, DryRun: *wire.DryRun}, nil
}

func uniqueValue(d *json.Decoder, depth int) error {
	if depth > 4 {
		return ErrIntent
	}
	token, err := d.Token()
	if err != nil {
		return ErrIntent
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delimiter != '{' {
		return ErrIntent
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return ErrIntent
		}
		seen[key] = true
		if err = uniqueValue(d, depth+1); err != nil {
			return err
		}
	}
	closing, err := d.Token()
	if err != nil || closing != json.Delim('}') {
		return ErrIntent
	}
	return nil
}
