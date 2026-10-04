package resend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_ListDomainsAndSend(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/domains":
			_, _ = w.Write([]byte(`{"data":[{"id":"dom_1","name":"enclii.dev","status":"verified","region":"us-east-1"}]}`))
		case "/emails":
			if r.Method == http.MethodPost {
				_, _ = w.Write([]byte(`{"id":"em_new"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"em_1","from":"noreply@enclii.dev","to":["a@b.com"],"subject":"Hi","created_at":"2026-01-01","last_event":"delivered"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "re_test", BaseURL: server.URL})
	domains, err := client.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 1 || domains[0].Name != "enclii.dev" {
		t.Fatalf("unexpected domains: %+v", domains)
	}
	if gotAuth != "Bearer re_test" {
		t.Fatalf("auth = %q", gotAuth)
	}

	emails, err := client.ListEmails(context.Background(), "enclii.dev")
	if err != nil || len(emails) != 1 {
		t.Fatalf("emails: %v err=%v", emails, err)
	}

	resp, err := client.SendEmail(context.Background(), SendEmailRequest{
		From: "Enclii <noreply@enclii.dev>", To: []string{"x@y.com"}, Subject: "t", Text: "body",
	})
	if err != nil || resp.ID != "em_new" {
		t.Fatalf("send: %+v err=%v", resp, err)
	}
}

func TestClient_SendEmail_ReplyToHeadersAndTags(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"id":"em_1"}`))
	}))
	defer server.Close()
	client := NewClient(Config{APIKey: "re_test", BaseURL: server.URL})

	_, err := client.SendEmail(context.Background(), SendEmailRequest{
		From: "Enclii <noreply@enclii.dev>", To: []string{"x@y.com"}, Subject: "t", Text: "body",
		ReplyTo: "support@madfam.io",
		Headers: map[string]string{"Auto-Submitted": "auto-generated"},
		Tags:    []Tag{{Name: "stream", Value: "transactional"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["reply_to"] != "support@madfam.io" {
		t.Fatalf("reply_to = %v", got["reply_to"])
	}
	if h, _ := got["headers"].(map[string]any); h["Auto-Submitted"] != "auto-generated" {
		t.Fatalf("headers = %v", got["headers"])
	}
	tags, _ := got["tags"].([]any)
	if len(tags) != 1 || tags[0].(map[string]any)["name"] != "stream" {
		t.Fatalf("tags = %v", got["tags"])
	}

	// Unset optional fields stay off the wire, so a plain send is unchanged.
	got = nil
	if _, err := client.SendEmail(context.Background(), SendEmailRequest{
		From: "Enclii <noreply@enclii.dev>", To: []string{"x@y.com"}, Subject: "t", Text: "body",
	}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"reply_to", "headers", "tags"} {
		if _, present := got[key]; present {
			t.Fatalf("%s present on a plain send: %v", key, got)
		}
	}
}

func TestClient_NotConfigured(t *testing.T) {
	client := NewClient(Config{})
	if client.Configured() {
		t.Fatal("expected unconfigured")
	}
	_, err := client.ListDomains(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}
