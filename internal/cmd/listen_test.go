package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestListenForwardsNewEventsSigned(t *testing.T) {
	forwarded := make(chan *http.Request, 4)
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded <- r
		w.WriteHeader(http.StatusOK)
	}))
	defer local.Close()

	queried := make(chan struct{}, 16)
	var mu sync.Mutex
	phase := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		p := phase
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch p {
		case 0:
			_, _ = w.Write([]byte(`{"data":[{"id":"019e0000-0000-7000-8000-000000000001","object":"event","type":"client.created","data":{}}],"has_more":false,"next_cursor":null}`))
		case 1:
			mu.Lock()
			phase = 2
			mu.Unlock()
			_, _ = w.Write([]byte(`{"data":[{"id":"019e0000-0000-7000-8000-000000000002","object":"event","type":"invoice.paid","data":{"invoice":{}}}],"has_more":false,"next_cursor":null}`))
		default:
			_, _ = w.Write([]byte(`{"data":[],"has_more":false,"next_cursor":null}`))
		}
		select {
		case queried <- struct{}{}:
		default:
		}
	}))
	defer api.Close()

	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", api.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	root := NewRootCmd()
	root.SetArgs([]string{"listen", "--forward-to", local.URL, "--poll-interval", "20ms", "--exit-after", "10s"})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	select {
	case <-queried:
	case <-time.After(3 * time.Second):
		t.Fatal("la query de establecimiento del watermark no llegó a tiempo")
	}

	mu.Lock()
	phase = 1
	mu.Unlock()

	select {
	case req := <-forwarded:
		if req.Header.Get("Factuarea-Signature") == "" {
			t.Error("falta Factuarea-Signature en el reenvío")
		}
		if got := req.Header.Get("Factuarea-Event-Type"); got != "invoice.paid" {
			t.Errorf("Factuarea-Event-Type = %q, quería invoice.paid", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("el evento nuevo no se reenvió a tiempo")
	}

	select {
	case req := <-forwarded:
		t.Fatalf("reenvío inesperado de un segundo evento (type=%q)", req.Header.Get("Factuarea-Event-Type"))
	case <-time.After(200 * time.Millisecond):
	}

	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("listen devolvió error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listen no terminó tras cancelar el contexto")
	}
}

func TestListenDoesNotReplayPreexistingEvents(t *testing.T) {
	forwarded := make(chan *http.Request, 8)
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded <- r
		w.WriteHeader(http.StatusOK)
	}))
	defer local.Close()

	queried := make(chan struct{}, 64)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("starting_after") == "" {
			_, _ = w.Write([]byte(`{"data":[{"id":"019e0000-0000-7000-8000-000000000001","object":"event","type":"client.created","data":{}},{"id":"019e0000-0000-7000-8000-000000000002","object":"event","type":"invoice.created","data":{"invoice":{}}},{"id":"019e0000-0000-7000-8000-000000000003","object":"event","type":"invoice.paid","data":{"invoice":{}}}],"has_more":false,"next_cursor":null}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[],"has_more":false,"next_cursor":null}`))
		}
		select {
		case queried <- struct{}{}:
		default:
		}
	}))
	defer api.Close()

	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", api.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	root := NewRootCmd()
	root.SetArgs([]string{"listen", "--forward-to", local.URL, "--poll-interval", "20ms", "--exit-after", "10s"})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	for i := 0; i < 3; i++ {
		select {
		case <-queried:
		case <-time.After(3 * time.Second):
			t.Fatal("listen no sondeó el feed a tiempo")
		}
	}

	select {
	case req := <-forwarded:
		t.Fatalf("se reenvió un evento pre-existente (type=%q); el watermark no se estableció al más reciente", req.Header.Get("Factuarea-Event-Type"))
	case <-time.After(200 * time.Millisecond):
	}

	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("listen devolvió error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listen no terminó tras cancelar el contexto")
	}
}

func TestValidateForwardToRejectsRemoteHost(t *testing.T) {
	err := validateForwardTo("http://evil.com/x", false)
	if err == nil {
		t.Fatal("esperaba error de loopback para host remoto sin --allow-remote-forward")
	}
}

func TestValidateForwardToAllowsLoopback(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:9000/hook", "http://localhost:4242/x", "https://[::1]:8080/y"} {
		if err := validateForwardTo(u, false); err != nil {
			t.Fatalf("loopback %s debería ser válido: %v", u, err)
		}
	}
}

func TestValidateForwardToAllowsRemoteWithFlag(t *testing.T) {
	if err := validateForwardTo("https://example.com/x", true); err != nil {
		t.Fatalf("con --allow-remote-forward el host remoto debería valer: %v", err)
	}
}

func TestParseEventsFilter(t *testing.T) {
	f := parseEventsFilter(" invoice.paid , client.created ,")
	if f == nil {
		t.Fatal("esperaba un filtro no nulo")
	}
	if !f["invoice.paid"] || !f["client.created"] {
		t.Fatalf("filtro incompleto: %v", f)
	}
	if parseEventsFilter("") != nil {
		t.Fatal("filtro vacío debería ser nil (todos)")
	}
}
