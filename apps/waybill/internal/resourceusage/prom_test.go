package resourceusage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPQuerier_QueryRangeSendsTheWindowAndParsesAMatrix(t *testing.T) {
	var form map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/prom/api/v1/query_range" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		form = map[string]string{}
		for k := range r.PostForm {
			form[k] = r.PostForm.Get(k)
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[
			{"metric":{"namespace":"shop","pod":"api-0","container":"app","node":"node-a"},
			 "values":[[1762236300,"0.5"],[1762236600,"NaN"],[1762236900,"0.25"]]}]}}`))
	}))
	defer srv.Close()

	q, err := NewHTTPQuerier(srv.URL+"/prom", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(1762236300, 0)
	end := start.Add(10 * time.Minute)
	got, err := q.QueryRange(context.Background(), `up{job="x"}`, start, end, Step)
	if err != nil {
		t.Fatal(err)
	}
	if form["query"] != `up{job="x"}` || form["start"] != "1762236300.000" || form["end"] != "1762236900.000" || form["step"] != "300" {
		t.Errorf("form = %v", form)
	}
	if form["timeout"] != "1m0s" {
		t.Errorf("timeout = %q", form["timeout"])
	}
	if len(got) != 1 || got[0].Labels["pod"] != "api-0" {
		t.Fatalf("series = %+v", got)
	}
	// NaN is not a measurement: dropped, not zero.
	if len(got[0].Points) != 2 || got[0].Points[0].V != 0.5 || got[0].Points[1].V != 0.25 {
		t.Errorf("points = %+v", got[0].Points)
	}
	if !got[0].Points[1].T.Equal(end) {
		t.Errorf("timestamp = %s, want %s", got[0].Points[1].T, end)
	}
}

func TestHTTPQuerier_QueryParsesAVector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = r.ParseForm()
		if r.PostForm.Get("time") != "1762322400.000" {
			t.Errorf("time = %q", r.PostForm.Get("time"))
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1762322400,"4"]}]}}`))
	}))
	defer srv.Close()

	q, _ := NewHTTPQuerier(srv.URL, time.Minute)
	got, err := q.Query(context.Background(), "count(up)", time.Unix(1762322400, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Points) != 1 || got[0].Points[0].V != 4 {
		t.Errorf("got %+v", got)
	}
}

// A Prometheus error is an error: the day is retried, never written empty.
func TestHTTPQuerier_ErrorsAreErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"api error":   {422, `{"status":"error","errorType":"execution","error":"query timed out"}`, "query timed out"},
		"not json":    {502, `<html>bad gateway</html>`, "HTTP 502"},
		"bad sample":  {200, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"x"]}]}}`, "malformed"},
		"result type": {200, `{"status":"success","data":{"resultType":"scalar","result":[{"metric":{},"value":[1,"1"]}]}}`, "unexpected result type"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			q, _ := NewHTTPQuerier(srv.URL, time.Minute)
			_, err := q.Query(context.Background(), "up", time.Now())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestNewHTTPQuerier_RejectsAnURLWithoutHost(t *testing.T) {
	if _, err := NewHTTPQuerier("not a url", time.Minute); err == nil {
		t.Error("accepted")
	}
}
