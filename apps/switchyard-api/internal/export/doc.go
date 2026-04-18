// Package export implements the P3.6 tenant data export pipeline.
//
// A tenant export is a single tarball that hands a customer back
// everything Enclii holds about one of their projects: K8s manifests,
// pg_dump of managed databases, R2 blob inventory (not contents),
// secret references (not values), and a project-scoped audit timeline.
//
// See docs/architecture/tenant-export.md for the scope, SLA, retention,
// auth model, and threat model.
//
// The package is deliberately small:
//
//	Service     — orchestrates the pipeline; lives in the API pod.
//	Builder     — assembles the tarball structure in memory / on disk
//	              with size-cap splitting.
//	k8sGatherer — scrubs and dumps K8s manifests for a project namespace.
//	pgGatherer  — runs pg_dump (or submits a K8s Job for large dumps).
//	r2Gatherer  — enumerates blobs, builds sha256 manifests.
//
// Secret values are never written to the tarball. Pre-signed URLs are
// never persisted in audit details.
package export
