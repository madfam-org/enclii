-- P3.2 Self-serve signup (Sprint 1)
--
-- Backs the /v1/signup surface: a user arrives at app.enclii.dev/signup,
-- enters their email, verifies it, connects GitHub via Janua OAuth, and is
-- dropped into their auto-provisioned project. Janua owns the user record;
-- this table is a per-signup state machine + audit trail.
--
-- Sprint 1 scope: email -> verified -> github linked -> project provisioned.
-- Sprint 2 adds framework auto-detect + billing capture; Sprint 3 adds team
-- invites and custom-domain claim. Schema here is forward-compatible with
-- those additions (extra nullable columns can be added without migration
-- churn).

CREATE TABLE IF NOT EXISTS public.signup_requests (
    id                              uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Identity
    email                           varchar(320) NOT NULL,
    company_name                    varchar(200),

    -- Janua coordination
    janua_user_sub                  varchar(255),   -- populated after Janua user is registered
    verification_token_hash         varchar(128),   -- sha256 hex of the token sent in verification email
    verification_token_expires_at   timestamptz,

    -- GitHub OAuth (linked via Janua; we never hold the raw access token in the DB)
    github_username                 varchar(255),
    github_access_token_secret_ref  varchar(255),   -- K8s Secret ref (namespace/name#key); never raw
    oauth_state_hash                varchar(128),   -- sha256 hex of the state param; validated on callback

    -- Lifecycle
    status                          varchar(32) NOT NULL DEFAULT 'pending_verification'
                                      CHECK (status IN (
                                          'pending_verification',
                                          'verified',
                                          'github_linked',
                                          'provisioning',
                                          'ready',
                                          'failed'
                                      )),
    provisioned_project_id          uuid REFERENCES public.projects(id) ON DELETE SET NULL,
    error_message                   text,

    -- Timestamps for each state transition (nullable; enforced by app code)
    email_verified_at               timestamptz,
    oauth_completed_at              timestamptz,
    provisioned_at                  timestamptz,

    created_at                      timestamptz NOT NULL DEFAULT now(),
    updated_at                      timestamptz NOT NULL DEFAULT now()
);

-- One active signup per email at a time. Once ready/failed, historical rows
-- are kept for audit but should not block a retry: we enforce "one row per
-- email in a non-terminal state" in application code, not via a unique
-- constraint (that would make re-signup-after-failure painful).
CREATE INDEX IF NOT EXISTS idx_signup_requests_email
    ON public.signup_requests(email);

CREATE INDEX IF NOT EXISTS idx_signup_requests_status
    ON public.signup_requests(status)
    WHERE status IN ('pending_verification','verified','github_linked','provisioning');

CREATE INDEX IF NOT EXISTS idx_signup_requests_verification_token
    ON public.signup_requests(verification_token_hash)
    WHERE verification_token_hash IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_signup_requests_oauth_state
    ON public.signup_requests(oauth_state_hash)
    WHERE oauth_state_hash IS NOT NULL;

COMMENT ON TABLE public.signup_requests IS
  'Per-signup state machine for self-serve signup (P3.2 Sprint 1). Email -> verified -> github_linked -> provisioning -> ready. Janua owns the user; we hold onboarding coordination state.';

COMMENT ON COLUMN public.signup_requests.status IS
  'pending_verification: verification email sent. verified: email link clicked. github_linked: OAuth completed. provisioning: Janua user + project being created. ready: terminal success. failed: terminal failure (see error_message).';

COMMENT ON COLUMN public.signup_requests.github_access_token_secret_ref IS
  'K8s Secret ref (e.g. enclii/signup-tokens#ghat-<uuid>) where the GitHub access token is stored. The raw token never lives in this DB. Following the RFC 0005 secret-ref convention used elsewhere.';


-- Append-only audit trail of every state transition. Useful for debugging
-- stuck signups and for future user-visible "where am I in the flow" UI.
CREATE TABLE IF NOT EXISTS public.signup_events (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    signup_request_id   uuid NOT NULL REFERENCES public.signup_requests(id) ON DELETE CASCADE,
    event_type          varchar(64) NOT NULL,  -- e.g. 'initiated', 'email_verified', 'github_linked', 'provisioned', 'failed'
    details             jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_signup_events_request
    ON public.signup_events(signup_request_id, created_at);

COMMENT ON TABLE public.signup_events IS
  'Append-only audit trail of signup state transitions. Never mutated; used for forensics on stuck signups and for the user-visible timeline.';
