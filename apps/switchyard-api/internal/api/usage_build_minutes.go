package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

// Build minutes, read from the meter instead of invented.
//
// WHAT THIS REPLACES
// ==================
// `calculateUsage` used to credit a flat 3.0 minutes per Release in the
// period and bill overage against that number at a per-minute rate. Three was
// not an average, an estimate or a placeholder anybody had measured — it was a
// literal, and every runner-SKU and per-product cost figure downstream
// inherited it. Waybill has aggregated real `build_minutes` from
// `build.completed` events since it was written; nothing asked it.
//
// UNAVAILABLE IS NOT ZERO
// =======================
// The single rule this file exists to enforce. If Waybill cannot be reached,
// the response says the meter could not be read — it does not report 0.0
// minutes used, because zero is indistinguishable from "a quiet month" on
// every dashboard and in every invoice review, and it is the one wrong answer
// that looks completely normal.

// waybillUsageTimeout bounds a single per-project read. Short: this sits on
// the dashboard's path behind usageHandlerBudget, and a slow meter must not
// become a slow page.
const waybillUsageTimeout = 5 * time.Second

// waybillUsageSummary is the subset of Waybill's UsageSummary this needs.
//
// Written out rather than imported: switchyard-api and waybill are separate Go
// modules and a shared struct would couple their release cycles for one map.
// The same reasoning the addon usage emitter records for its own payload type.
type waybillUsageSummary struct {
	Metrics map[string]float64 `json:"metrics"`
}

// buildMinutesResult is what the meter said, and whether it managed to say it.
type buildMinutesResult struct {
	Minutes float64
	// Known is false when ANY project's read failed. Deliberately
	// all-or-nothing: a partial sum is a smaller number that looks like a
	// complete one, and there is no field on the response that could carry
	// "this is 6 projects out of 8" in a way a reader would notice.
	Known bool
	// Reason is a short, non-sensitive explanation for the response. Never
	// carries an upstream body — Waybill's errors can echo the request.
	Reason string
}

// fetchBuildMinutes sums Waybill's aggregated build minutes across projects.
//
// Keeps the fan-out shape the release-counting loop had: an errgroup capped at
// usageFanoutConcurrency, so N projects cost ~ceil(N/cap) round trips rather
// than N.
//
// The period is not a parameter. Waybill's only usage read is
// `/usage/current`, which is month-to-date, and the caller's period is the
// calendar month — the same window. Passing a period this endpoint cannot
// honour would be a parameter that silently does nothing.
func (h *Handler) fetchBuildMinutes(ctx context.Context, projectIDs []uuid.UUID) buildMinutesResult {
	if h.billingProxy == nil || h.billingProxy.WaybillURL() == "" {
		return buildMinutesResult{
			Known:  false,
			Reason: "billing service not configured",
		}
	}
	if len(projectIDs) == 0 {
		// No projects is a real, knowable zero — unlike an unreachable meter.
		return buildMinutesResult{Minutes: 0, Known: true}
	}

	var (
		mu     sync.Mutex
		total  float64
		failed int
	)

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(usageFanoutConcurrency)
	for _, id := range projectIDs {
		id := id
		g.Go(func() error {
			minutes, err := h.waybillBuildMinutes(gCtx, id)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				return nil
			}
			total += minutes
			return nil
		})
	}
	_ = g.Wait()

	if failed > 0 {
		return buildMinutesResult{
			Known: false,
			// Counts only. A project id is not secret, but naming which
			// projects failed puts a list of tenant identifiers into a
			// response that any authenticated operator can read, for no
			// operational gain over the count.
			Reason: fmt.Sprintf("meter unreachable for %d of %d projects", failed, len(projectIDs)),
		}
	}
	return buildMinutesResult{Minutes: total, Known: true}
}

// waybillBuildMinutes reads one project's month-to-date build minutes.
func (h *Handler) waybillBuildMinutes(ctx context.Context, projectID uuid.UUID) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, waybillUsageTimeout)
	defer cancel()

	url := fmt.Sprintf("%s/api/v1/projects/%s/usage/current", h.billingProxy.WaybillURL(), projectID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("build waybill request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "switchyard-usage/1.0")
	if h.billingProxy.InternalAPIKey != "" {
		req.Header.Set("X-API-Key", h.billingProxy.InternalAPIKey)
	}

	client := &http.Client{Timeout: waybillUsageTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("waybill unreachable")
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, fmt.Errorf("read waybill response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Status only — the body of an authenticated internal endpoint can
		// echo the request.
		return 0, fmt.Errorf("waybill returned HTTP %d", resp.StatusCode)
	}

	var summary waybillUsageSummary
	if err := json.Unmarshal(body, &summary); err != nil {
		return 0, fmt.Errorf("waybill response is not a usage summary")
	}

	// `build_minutes` is Waybill's MetricBuildMinutes. A project with no
	// builds this month has no such key, which is a real zero — the
	// difference from an unreachable meter is that the read SUCCEEDED.
	return summary.Metrics[waybillBuildMinutesMetric], nil
}

// waybillBuildMinutesMetric is the metric_type string Waybill's hourly
// aggregator writes for build duration (events.MetricBuildMinutes). A literal
// across the module boundary, like the resource_type the addon emitter pins.
const waybillBuildMinutesMetric = "build_minutes"

// distinctProjectIDs collapses a service list to the projects behind it.
// Waybill aggregates per project, so N services in one project is one read.
func distinctProjectIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// buildMinutesMetric renders the meter reading as a UsageMetric.
//
// When the meter could not be read the metric reports Used: 0 with
// Unavailable: true and Cost: 0 — the zero is inert and explicitly marked,
// rather than a silent claim that no minutes were used. A consumer that
// ignores the flag gets an obviously suspicious zero; a consumer that reads it
// gets the truth.
func buildMinutesMetric(result buildMinutesResult) UsageMetric {
	m := UsageMetric{
		Type:     "build",
		Label:    "Build Minutes",
		Included: includedBuild,
		Unit:     "minutes",
		Source:   "waybill",
	}
	if !result.Known {
		m.Unavailable = true
		m.Note = result.Reason
		return m
	}
	m.Used = roundToTwoDecimals(result.Minutes)
	m.Cost = roundToTwoDecimals(calculateOverage(result.Minutes, includedBuild, buildPerMinute))
	return m
}
