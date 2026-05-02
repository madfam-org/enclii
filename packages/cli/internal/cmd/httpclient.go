package cmd

import (
	"net/http"
	"time"
)

// defaultRequestTimeout is the cap for any single CLI HTTP call to the
// Switchyard API. Long-running operations (log tailing, exports) carry their
// own context deadlines on top of this.
const defaultRequestTimeout = 30 * time.Second

// httpClient returns a fresh *http.Client with an explicit Timeout. Use this
// instead of http.DefaultClient anywhere the CLI talks to the API: the global
// default has no timeout, so a hung server would hang the CLI forever.
func httpClient() *http.Client {
	return &http.Client{Timeout: defaultRequestTimeout}
}

// httpClientForDownload returns a client with no Client.Timeout so large
// transfers can run for as long as the request's context allows. The caller
// MUST pass a context with a deadline (use context.WithTimeout) — this
// helper does not protect against hung connections on its own.
func httpClientForDownload() *http.Client {
	return &http.Client{}
}
