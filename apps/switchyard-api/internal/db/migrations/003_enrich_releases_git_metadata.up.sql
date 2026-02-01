-- Add git metadata columns to releases table
-- These fields capture branch, commit, and PR info from webhook events
ALTER TABLE releases ADD COLUMN IF NOT EXISTS git_branch VARCHAR(255);
ALTER TABLE releases ADD COLUMN IF NOT EXISTS commit_message TEXT;
ALTER TABLE releases ADD COLUMN IF NOT EXISTS commit_author_name VARCHAR(255);
ALTER TABLE releases ADD COLUMN IF NOT EXISTS commit_author_email VARCHAR(255);
ALTER TABLE releases ADD COLUMN IF NOT EXISTS pr_number INTEGER;
ALTER TABLE releases ADD COLUMN IF NOT EXISTS pr_title TEXT;
ALTER TABLE releases ADD COLUMN IF NOT EXISTS pr_url VARCHAR(500);
ALTER TABLE releases ADD COLUMN IF NOT EXISTS repo_url VARCHAR(500);
