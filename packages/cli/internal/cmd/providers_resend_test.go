package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

// The server's send-test handlers read the recipient from args.to and answer
// invalid_request without it (operator_provider_resend_dns.go). Before --to
// existed the verb could not succeed from the CLI at all.
func TestProviderResendSendTestApply_SendsRecipientAsArgsTo(t *testing.T) {
	var seenPath string
	var body operationRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.Method + " " + r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"operation":"providers.resend.send-test-apply","status":"ready_to_apply","dry_run":true}`))
	}))
	defer srv.Close()

	cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "tok"}
	root := NewRootCommand(cfg)
	root.SetArgs([]string{"providers", "resend", "send-test-apply", "enclii.dev", "--to", " ops@example.com "})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	require.NoError(t, root.Execute())
	assert.Equal(t, "POST /v1/providers/resend/send-test-apply", seenPath)
	assert.Equal(t, "providers.resend.send-test-apply", body.Operation)
	assert.True(t, body.DryRun, "without --apply the request stays a dry-run")
	assert.Equal(t, "ops@example.com", body.Args["to"])
	assert.Equal(t, "enclii.dev", body.Args["target"])
}

func TestProviderResendSendTestApply_RejectsBadRecipientBeforeCallingAPI(t *testing.T) {
	cases := map[string][]string{
		"missing":      {},
		"blank":        {"--to", "   "},
		"not an email": {"--to", "ops"},
		"display name": {"--to", "Ops <ops@example.com>"},
		"list":         {"--to", "a@example.com,b@example.com"},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			called := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			cfg := &config.Config{APIEndpoint: srv.URL, APIToken: "tok"}
			root := NewRootCommand(cfg)
			root.SetArgs(append([]string{"providers", "resend", "send-test-apply", "enclii.dev"}, extra...))
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)

			err := root.Execute()
			require.Error(t, err)
			var vErr *exitcodes.ValidationError
			assert.True(t, errors.As(err, &vErr), "want a ValidationError, got %T: %v", err, err)
			assert.Contains(t, err.Error(), "--to")
			assert.False(t, called, "an invalid recipient must not reach the API")
		})
	}
}

// --to belongs to send-test-apply only; the other resend mutating verbs take
// the domain positionally and must not grow an unrelated recipient flag.
func TestProviderResend_ToFlagOnlyOnSendTest(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	resend := findSubcommand(NewProvidersCommand(cfg), "resend")
	require.NotNil(t, resend)

	for _, sub := range resend.Commands() {
		if sub.Name() == "send-test-apply" {
			assert.NotNil(t, sub.Flags().Lookup("to"), "send-test-apply must offer --to")
			continue
		}
		assert.Nil(t, sub.Flags().Lookup("to"), "%s must not offer --to", sub.Name())
	}
}
