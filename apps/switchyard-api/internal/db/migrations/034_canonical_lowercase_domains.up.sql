-- 034_canonical_lowercase_domains
--
-- Hostnames are stored canonically lowercased.
--
-- custom_domains.domain and junctions.domain are plain varchar(255) with
-- case-sensitive btree indexes, and every ownership gate on the Cloudflare for
-- SaaS path is a `WHERE domain = $1`. Cloudflare, meanwhile, matches hostnames
-- case-insensitively. A row stored as "App.Victim.com" was therefore invisible
-- to a lookup for "app.victim.com" while naming the same registration at the
-- edge — enough to claim, and then delete, another tenant's live custom
-- hostname.
--
-- The code now canonicalises on write and compares with lower() on read. This
-- migration brings the rows already in the table into that form so the two
-- agree.
--
-- Verified against the live estate before writing: 0 of the declared hostnames
-- carry an uppercase character, so this is expected to update 0 rows. It is
-- written to be correct anyway, because a single mixed-case row reopens the
-- hole.

-- Collisions first, and loudly.
--
-- Scope note, because the first version of this check got it wrong: it grouped
-- by (service_id, environment_id, lower(domain)) — the shape of
-- custom_domains_service_id_environment_id_domain_key — and so only ever saw a
-- case pair held twice by ONE service. But nothing in the schema stops two
-- different services, in two different PROJECTS, from holding the same
-- hostname. That pair raises no unique violation, so the migration ran clean,
-- said nothing, and silently merged a cross-tenant collision. Worse than
-- silent: `SET domain = lower(domain)` rewrites the updated row as a new heap
-- version, so the victim's tuple moved to the tail and the attacker's row
-- started answering an unordered `WHERE lower(domain) = ...` first. The
-- hostname changed hands with no error anywhere.
--
-- So the question asked here is the whole question: does more than one
-- SPELLING of a hostname exist anywhere in the table? count(DISTINCT domain)
-- rather than count(*) is deliberate — rows that already share an identical
-- domain string are a pre-existing state this migration neither creates nor
-- worsens, whereas two spellings collapsing into one is exactly what it would
-- create.
--
-- Refuse and name them. Two records claiming one hostname is the harm this
-- migration exists to close, and the right answer is an operator deciding which
-- claim is real, never this script picking one and rewriting the other.
DO $$
DECLARE
    conflicting text;
BEGIN
    SELECT string_agg(detail, '; ' ORDER BY detail)
      INTO conflicting
      FROM (
          SELECT lower(domain) || ' spelled as [' || string_agg(DISTINCT domain, ', ') ||
                 '] by service_id(s) [' || string_agg(DISTINCT service_id::text, ', ') || ']' AS detail
            FROM custom_domains
           GROUP BY lower(domain)
          HAVING count(DISTINCT domain) > 1
      ) AS dupes;

    IF conflicting IS NOT NULL THEN
        RAISE EXCEPTION
            'custom_domains holds hostnames that differ only by case (%). '
            'Lowercasing would merge them into one hostname held by more than one record, '
            'across services and therefore possibly across projects. '
            'Resolve the duplicate claims before applying migration 034.',
            conflicting;
    END IF;

    -- junctions: same question. The unique index here is (domain, path), so a
    -- case pair on two different paths raises no violation either — and it is
    -- still two projects able to claim one hostname, because every ownership
    -- lookup keys on lower(domain) alone and ignores the path.
    SELECT string_agg(detail, '; ' ORDER BY detail)
      INTO conflicting
      FROM (
          SELECT lower(domain) || ' spelled as [' || string_agg(DISTINCT domain, ', ') ||
                 '] by project_id(s) [' || string_agg(DISTINCT project_id::text, ', ') || ']' AS detail
            FROM junctions
           GROUP BY lower(domain)
          HAVING count(DISTINCT domain) > 1
      ) AS dupes;

    IF conflicting IS NOT NULL THEN
        RAISE EXCEPTION
            'junctions holds hostnames that differ only by case (%). '
            'Lowercasing would merge them into one hostname served by more than one junction, '
            'across projects. Resolve the duplicate claims before applying migration 034.',
            conflicting;
    END IF;
END
$$;

UPDATE custom_domains
   SET domain = lower(domain),
       updated_at = NOW()
 WHERE domain <> lower(domain);

UPDATE junctions
   SET domain = lower(domain),
       updated_at = NOW()
 WHERE domain <> lower(domain);

COMMENT ON COLUMN custom_domains.domain IS
    'Hostname, stored canonically lowercased. DNS is case-insensitive; lookups use lower(domain) and writes canonicalise, so a case variant can never read as a different hostname.';
COMMENT ON COLUMN junctions.domain IS
    'Hostname, stored canonically lowercased. See custom_domains.domain.';

-- NOT migrated by this script: the tunnel ingress configuration.
--
-- The cloudflared ingress rules are the platform's other hostname-keyed store,
-- and they live outside this database — in a Kubernetes ConfigMap, or in
-- Cloudflare's own tunnel configuration API, depending on which
-- TunnelRoutesManager is wired up. A migration cannot reach either, so this
-- script deliberately leaves them alone rather than pretending otherwise.
--
-- Why that is safe today, stated so the next person can re-check it rather than
-- trust it: every hostname the platform can declare was parsed out of the
-- estate's manifests and counted. 91 distinct declared hostnames, 0 of them
-- carrying an uppercase character. With no mixed-case rule in the set, there is
-- nothing for a rewrite to fix.
--
-- Why it is not left to luck either: a pre-existing mixed-case rule would have
-- survived RemoveRoute (which compared byte-exact and would not have found it)
-- and then gained a duplicate from AddRoute. So the comparisons themselves were
-- made case-insensitive — see services.CanonicalHostname and its callers — and
-- AddRoute now writes the canonical form. A mixed-case rule that predates this
-- change is therefore matched, replaced and canonicalised by the first
-- reconciliation that touches it, without a migration.
