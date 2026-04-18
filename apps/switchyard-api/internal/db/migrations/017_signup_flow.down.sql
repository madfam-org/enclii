-- Revert P3.2 self-serve signup.

DROP INDEX IF EXISTS public.idx_signup_events_request;
DROP TABLE IF EXISTS public.signup_events;

DROP INDEX IF EXISTS public.idx_signup_requests_oauth_state;
DROP INDEX IF EXISTS public.idx_signup_requests_verification_token;
DROP INDEX IF EXISTS public.idx_signup_requests_status;
DROP INDEX IF EXISTS public.idx_signup_requests_email;
DROP TABLE IF EXISTS public.signup_requests;
