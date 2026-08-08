package api

// Who may be served on a hostname.
//
// A hostname string is never evidence of ownership: the Cloudflare for SaaS
// fallback-origin zone is shared by every tenant, and the tunnel ingress config
// is keyed on hostname across every tenant too. Both of the platform's
// hostname-bearing records therefore have to be consulted, and the answer is
// used at three different strengths, which is why these live together:
//
//	assertHostnameClaimableBy             custom-hostname path   strict
//	assertHostnameNotHeldByAnotherProject  declaration           strict
//	zonePathHostnameConflict              zone deploy path       conflict-only
//
// "Strict" means an unresolvable lookup refuses. Only the deploy path's zone
// branch lets an unknown answer through, and only because nothing is created
// there — see the note on each function.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// hostnameHeldError marks a refusal caused by another project POSITIVELY
// holding the hostname, as distinct from a refusal caused by not being able to
// tell who holds it.
//
// The two must not be answered alike. "Another project has it" is a settled
// fact the caller can act on — 409, and the message names the holder. "The
// lookup failed" is our problem, not theirs, and reporting it as a conflict
// tells a rightful owner their own hostname belongs to someone else. Since the
// claim now also carries the insert, an ordinary write failure would otherwise
// have been rendered as a conflict too, with the raw database error in the body.
type hostnameHeldError struct{ msg string }

func (e *hostnameHeldError) Error() string { return e.msg }

// heldByAnotherProject builds a refusal that isHostnameHeld recognises.
func heldByAnotherProject(format string, args ...interface{}) error {
	return &hostnameHeldError{msg: fmt.Sprintf(format, args...)}
}

// isHostnameHeld reports whether err is a positive ownership conflict.
func isHostnameHeld(err error) bool {
	var held *hostnameHeldError
	return errors.As(err, &held)
}

// hostnameOwners resolves every project that holds a record entitling it to be
// served on a hostname.
//
// Two kinds of record confer that entitlement and BOTH have to be consulted:
//
//   - a custom_domains row, created by the domain API and by the deploy path;
//   - a junctions row, created by the junction API. Junctions provision edge
//     infrastructure — a tunnel ingress rule, and on this path a Cloudflare
//     custom hostname — without ever writing a custom_domains row. Keying
//     ownership off custom_domains alone therefore reported a live,
//     junction-served client hostname as unowned, and "unowned" is the one
//     answer that lets another project adopt it.
//
// An empty slice means nobody holds the hostname. It never means "could not
// tell": that is an error and every caller fails closed on it.
func (h *Handler) hostnameOwners(ctx context.Context, domain string) ([]uuid.UUID, error) {
	if h == nil || h.repos == nil || h.repos.CustomDomains == nil ||
		h.repos.Services == nil || h.repos.Junctions == nil {
		return nil, fmt.Errorf(
			"cannot establish which project owns %s: the domain repositories are unavailable", domain)
	}

	var owners []uuid.UUID

	// EVERY custom_domains row for the hostname, not the first one the heap
	// yields. The uniqueness on custom_domains.domain is scoped to one service,
	// so two projects can hold rows for the same hostname — and when they do,
	// reading only one of them reports the other as unowned. That is the answer
	// that lets a caller claim, and then delete, a hostname it does not hold.
	records, err := h.repos.CustomDomains.ListByDomain(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to look up the owner of %s: %w", domain, err)
	}
	for i := range records {
		record := &records[i]
		service, svcErr := h.repos.Services.GetByID(record.ServiceID)
		if svcErr != nil {
			return nil, fmt.Errorf("failed to resolve the project that owns %s: %w", domain, svcErr)
		}
		if service == nil {
			return nil, fmt.Errorf(
				"custom domain %s references service %s, which no longer exists", domain, record.ServiceID)
		}
		if service.ProjectID == uuid.Nil || containsProjectID(owners, service.ProjectID) {
			continue
		}
		owners = append(owners, service.ProjectID)
	}

	junctionOwners, err := h.repos.Junctions.ProjectIDsByDomain(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve the junctions serving %s: %w", domain, err)
	}
	for _, projectID := range junctionOwners {
		if projectID == uuid.Nil || containsProjectID(owners, projectID) {
			continue
		}
		owners = append(owners, projectID)
	}

	return owners, nil
}

func containsProjectID(ids []uuid.UUID, want uuid.UUID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// firstForeignOwner returns the first owner that is not projectID, and whether
// there was one.
func firstForeignOwner(owners []uuid.UUID, projectID uuid.UUID) (uuid.UUID, bool) {
	for _, id := range owners {
		if id != projectID {
			return id, true
		}
	}
	return uuid.Nil, false
}

// assertHostnameClaimableBy refuses when any other project already holds a
// record for this hostname.
//
// The fallback-origin zone is shared, so without this check any project could
// register — or silently adopt — a hostname another project is already being
// served on, and have its own row marked verified off the back of the other
// project's certificate.
func (h *Handler) assertHostnameClaimableBy(ctx context.Context, domain string, owner *domainOwner) error {
	if owner == nil || owner.ProjectID == uuid.Nil {
		return fmt.Errorf(
			"refusing to provision a custom hostname for %s: the claiming project could not be determined", domain)
	}

	owners, err := h.hostnameOwners(ctx, domain)
	if err != nil {
		return err
	}
	if foreign, ok := firstForeignOwner(owners, owner.ProjectID); ok {
		return heldByAnotherProject(
			"refusing to claim custom hostname %s for project %s: it is already registered to project %s",
			domain, owner.ProjectID, foreign)
	}

	return nil
}

// assertHostnameNotHeldByAnotherProject refuses when a DIFFERENT project holds
// a record for the hostname, and refuses when it cannot tell.
//
// This is the DECLARATION-time gate — AddCustomDomain, AddServiceDomain,
// CreateJunction. It has to fail closed on an unresolvable lookup, because the
// record it is about to let the caller create is itself an ownership claim:
// minting one for a hostname another project already serves does not only
// overwrite that project's routing, it makes the hostname permanently contested
// and gets the RIGHTFUL owner's own provisioning refused from then on. A 500 on
// a declaration during a database blip is the cheaper failure.
func (h *Handler) assertHostnameNotHeldByAnotherProject(ctx context.Context, domain string, owner *domainOwner) error {
	if owner == nil || owner.ProjectID == uuid.Nil {
		return fmt.Errorf(
			"refusing to register %s: the claiming project could not be determined", domain)
	}

	owners, err := h.hostnameOwners(ctx, domain)
	if err != nil {
		return err
	}
	if foreign, ok := firstForeignOwner(owners, owner.ProjectID); ok {
		return heldByAnotherProject(
			"refusing to register %s for project %s: it is already registered to project %s",
			domain, owner.ProjectID, foreign)
	}
	return nil
}

// zonePathHostnameConflict is the same question asked on the DEPLOY path, where
// the answer is used only to withhold an ingress write.
//
// It is deliberately weaker: it refuses on a positive conflict and lets an
// unresolvable lookup through. Nothing is created here — the records already
// exist — so an unknown answer cannot mint a competing claim, and blocking
// every MADFAM deploy on a transient repository error costs more than the
// narrow overwrite it would prevent. The custom-hostname path, which reaches
// client-owned hostnames on a shared zone, keeps the strict check
// (assertHostnameClaimableBy).
func (h *Handler) zonePathHostnameConflict(ctx context.Context, domain string, owner *domainOwner) error {
	if owner == nil || owner.ProjectID == uuid.Nil {
		return nil
	}

	owners, err := h.hostnameOwners(ctx, domain)
	if err != nil {
		h.logger.Warn(ctx, "Could not establish who holds this hostname before writing its ingress rule",
			logging.String("domain", domain),
			logging.Error("error", err))
		return nil
	}
	if foreign, ok := firstForeignOwner(owners, owner.ProjectID); ok {
		return heldByAnotherProject(
			"refusing to route %s to project %s: it is already registered to project %s",
			domain, owner.ProjectID, foreign)
	}
	return nil
}
