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
