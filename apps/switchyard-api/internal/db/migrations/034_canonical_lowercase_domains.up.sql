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
-- Lowercasing two rows onto the same key would violate
-- custom_domains_service_id_environment_id_domain_key or
-- idx_junctions_domain_path. Either would mean two records already claim one
-- hostname, which is exactly the harm this migration exists to close: the right
-- answer is an operator deciding which claim is real, never this script picking
-- one and deleting the other. Refuse and name the rows.
DO $$
DECLARE
    conflicting text;
BEGIN
    SELECT string_agg(DISTINCT lowered, ', ')
      INTO conflicting
      FROM (
          SELECT lower(domain) AS lowered
            FROM custom_domains
           GROUP BY service_id, environment_id, lower(domain)
          HAVING count(*) > 1
      ) AS dupes;

    IF conflicting IS NOT NULL THEN
        RAISE EXCEPTION
            'custom_domains holds hostnames that differ only by case within one service+environment (%). '
            'Two records claim one hostname; resolve the duplicate claims before applying migration 034.',
            conflicting;
    END IF;

    SELECT string_agg(DISTINCT lowered, ', ')
      INTO conflicting
      FROM (
          SELECT lower(domain) AS lowered
            FROM junctions
           GROUP BY lower(domain), path
          HAVING count(*) > 1
      ) AS dupes;

    IF conflicting IS NOT NULL THEN
        RAISE EXCEPTION
            'junctions holds domain+path pairs that differ only by case (%). '
            'Two junctions claim one hostname+path; resolve the duplicate claims before applying migration 034.',
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
