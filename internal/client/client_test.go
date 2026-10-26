package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa",
		WithBaseURL(srv.URL),
		WithMaxRetries(2),
		WithSleep(func(time.Duration) {}),
	)
	return c, srv
}

func TestDoSendsBearerAndReturnsBody(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer fact_test_aaaaaaaaaaaaaaaaaaaaaaaa" {
			t.Errorf("bad auth header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req_123")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	})
	resp, err := c.Do(context.Background(), "GET", "/v1/account", nil, nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != 200 || string(resp.Body) != `{"id":"x"}` {
		t.Fatalf("unexpected resp: %d %s", resp.StatusCode, resp.Body)
	}
	if resp.RequestID != "req_123" {
		t.Fatalf("missing request id: %q", resp.RequestID)
	}
}

func TestDoParsesErrorEnvelope(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"invoice_invalid_total","message":"Total inválido","request_id":"req_9"}}`))
	})
	_, err := c.Do(context.Background(), "POST", "/v1/invoices", []byte(`{}`), nil)
	var api *apierr.APIError
	if !errors.As(err, &api) {
		t.Fatalf("expected APIError, got %T %v", err, err)
	}
	if api.Type != "invalid_request_error" || api.Code != "invoice_invalid_total" || api.StatusCode != 422 {
		t.Fatalf("bad parsed error: %+v", api)
	}
}

func TestDoNonJSONErrorBecomesGenericAPIError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`<html>service unavailable</html>`))
	})
	_, err := c.Do(context.Background(), "GET", "/v1/account", nil, nil)
	var api *apierr.APIError
	if !errors.As(err, &api) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if api.Type != "service_unavailable_error" || api.StatusCode != 503 {
		t.Fatalf("bad synthesized error: %+v", api)
	}
}

func TestDoRetriesOn500ThenSucceeds(t *testing.T) {
	var calls int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	resp, err := c.Do(context.Background(), "GET", "/v1/account", nil, nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != 200 || atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls and 200, got %d calls / %d", calls, resp.StatusCode)
	}
}

func TestDoAddsIdempotencyKeyOnPOST(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.Header.Get("Idempotency-Key") == "" {
			t.Error("POST without Idempotency-Key")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := c.Do(context.Background(), "POST", "/v1/invoices", []byte(`{}`), nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
}

func TestWithoutRetryMakesASingleCallAndDoesNotMutateTheSharedClient(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	shared := New("fact_test_x", WithBaseURL(srv.URL), WithSleep(func(time.Duration) {}))

	if _, err := shared.WithoutRetry().Do(context.Background(), "POST", "/v1/x", nil, nil); err == nil {
		t.Fatal("esperaba el 503 como error")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("sin reintento: %d llamadas, quiero 1", n)
	}

	calls.Store(0)
	if _, err := shared.Do(context.Background(), "POST", "/v1/x", nil, nil); err == nil {
		t.Fatal("esperaba el 503 como error")
	}
	if n := calls.Load(); n != 4 {
		t.Fatalf("el cliente compartido debe seguir reintentando: %d llamadas, quiero 4", n)
	}
}

func TestNotModifiedIsSuccessWithoutBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != `"abc"` {
			t.Errorf("If-None-Match = %q", r.Header.Get("If-None-Match"))
		}
		w.Header().Set("ETag", `"abc"`)
		w.WriteHeader(http.StatusNotModified)
	}))
	t.Cleanup(srv.Close)
	c := New("fact_test_x", WithBaseURL(srv.URL))
	resp, err := c.Do(context.Background(), "GET", "/v1/x", nil, map[string]string{"If-None-Match": `"abc"`})
	if err != nil {
		t.Fatalf("un 304 no es un error: %v", err)
	}
	if resp.StatusCode != 304 || len(resp.Body) != 0 || resp.Header.Get("ETag") != `"abc"` {
		t.Fatalf("respuesta: %+v", resp)
	}
}

func TestRetryAfterIsKeptInTheError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	var slept []time.Duration
	c := New("fact_test_x", WithBaseURL(srv.URL), WithSleep(func(d time.Duration) { slept = append(slept, d) }))
	_, err := c.Do(context.Background(), "GET", "/v1/x", nil, nil)
	var api *apierr.APIError
	if !errors.As(err, &api) || api.StatusCode != 503 || api.RetryAfter != "7" {
		t.Fatalf("error = %#v", err)
	}
	if len(slept) != 3 || slept[0] != 7*time.Second {
		t.Errorf("los reintentos respetan Retry-After: %v", slept)
	}
}
