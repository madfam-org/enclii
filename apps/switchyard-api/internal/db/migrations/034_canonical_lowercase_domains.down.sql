-- Reverting 034 restores the column comments only.
--
-- The original casing of a hostname is not recoverable and is not worth
-- recovering: it carried no information (DNS is case-insensitive) and the
-- mixed-case rows it would restore are the cross-tenant hazard the up
-- migration removed. Reverting the code without reverting the data is safe —
-- lowercase rows match a case-sensitive `domain = $1` for lowercase input,
-- which is what every caller sends.

COMMENT ON COLUMN custom_domains.domain IS NULL;
COMMENT ON COLUMN junctions.domain IS NULL;
