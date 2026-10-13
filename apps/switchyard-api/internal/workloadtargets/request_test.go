package workloadtargets

import (
	"strings"
	"testing"
)

func TestApplyRequestRejectsAmbiguousIntentAndUnboundedInput(t *testing.T) {
	valid := `{"dry_run":false,"expect_plan_sha256":"` + strings.Repeat("a", 64) + `","reason":"fixed fixture","idempotency_key":"11000000-aaaa-4000-8000-000000000003","scope":{"project":"fixture","service":"11000000-0000-4000-8000-000000000002","environment":"production"},"args":{"namespace":"fixture","deployment":"fixture-web"}}`
	if _, err := ParseRequest([]byte(valid)); err != nil {
		t.Fatal("closed apply refused")
	}
	for name, body := range map[string]string{
		"hash absent":                  strings.Replace(valid, `"expect_plan_sha256":"`+strings.Repeat("a", 64)+`",`, "", 1),
		"old hash alias":               strings.Replace(valid, "expect_plan_sha256", "expect_plan", 1),
		"reason absent":                strings.Replace(valid, `"reason":"fixed fixture",`, "", 1),
		"blank reason":                 strings.Replace(valid, "fixed fixture", "   ", 1),
		"control reason":               strings.Replace(valid, "fixed fixture", `fixed\nfixture`, 1),
		"reason too long":              strings.Replace(valid, "fixed fixture", strings.Repeat("r", 1001), 1),
		"id absent":                    strings.Replace(valid, `"idempotency_key":"11000000-aaaa-4000-8000-000000000003",`, "", 1),
		"uppercase UUID":               strings.Replace(valid, "aaaa", "AAAA", 1),
		"nil UUID":                     strings.Replace(valid, "11000000-aaaa-4000-8000-000000000003", "00000000-0000-0000-0000-000000000000", 1),
		"duplicate hash":               strings.Replace(valid, `"expect_plan_sha256":`, `"expect_plan_sha256":"`+strings.Repeat("b", 64)+`","expect_plan_sha256":`, 1),
		"caller selector":              strings.Replace(valid, `"deployment":"fixture-web"`, `"deployment":"fixture-web","selector":"app=fixture"`, 1),
		"unknown outer":                strings.Replace(valid, `{"dry_run"`, `{"raw_sql":"invalid","dry_run"`, 1),
		"wrong operation":              strings.Replace(valid, `{"dry_run"`, `{"operation":"ops.apps.sync","dry_run"`, 1),
		"dry run with apply authority": strings.Replace(valid, `"dry_run":false`, `"dry_run":true`, 1),
		"oversize":                     strings.Repeat("x", 8193),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseRequest([]byte(body)); err == nil {
				t.Fatal("ambiguous authority accepted")
			}
		})
	}
}
