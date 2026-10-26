-- Downgrade cannot erase encrypted backups, unresolved dispatch fences or
-- append-only evidence. Preserve every row and require explicit forward repair.
DO $$ BEGIN
    RAISE EXCEPTION 'argocd protected backup journal downgrade refused; preserve encrypted operations and evidence';
END $$;
