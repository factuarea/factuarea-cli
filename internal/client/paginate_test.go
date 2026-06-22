package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func TestPaginateFollowsCursor(t *testing.T) {
	page := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("starting_after") == "" {
			_, _ = fmt.Fprint(w, `{"data":[{"id":"a"},{"id":"b"}],"has_more":true,"next_cursor":"b"}`)
		} else {
			_, _ = fmt.Fprint(w, `{"data":[{"id":"c"}],"has_more":false,"next_cursor":null}`)
		}
		page++
	})
	var ids []string
	err := c.Paginate(context.Background(), "/v1/invoices", url.Values{}, func(item json.RawMessage) error {
		var o struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(item, &o)
		ids = append(ids, o.ID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids) != "[a b c]" {
		t.Fatalf("got %v", ids)
	}
	if page != 2 {
		t.Fatalf("expected 2 page requests, got %d", page)
	}
}

// TestPaginateDegradesToSinglePage cubre los catálogos enum como
// /v1/taxes/active que devuelven {data:[...]} sin has_more/next_cursor:
// debe emitir una sola página y terminar, no bucle-infinito.
func TestPaginateDegradesToSinglePage(t *testing.T) {
	page := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		page++
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":[{"id":"iva21"},{"id":"iva10"}]}`)
	})
	var ids []string
	err := c.Paginate(context.Background(), "/v1/taxes/active", url.Values{}, func(item json.RawMessage) error {
		var o struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(item, &o)
		ids = append(ids, o.ID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids) != "[iva21 iva10]" {
		t.Fatalf("got %v", ids)
	}
	if page != 1 {
		t.Fatalf("expected exactly 1 page request, got %d", page)
	}
}

// TestPaginatePropagatesEachError verifica que un error de la callback corta la
// iteración y se propaga sin pedir más páginas.
func TestPaginatePropagatesEachError(t *testing.T) {
	page := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		page++
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":[{"id":"a"}],"has_more":true,"next_cursor":"a"}`)
	})
	sentinel := errors.New("stop")
	err := c.Paginate(context.Background(), "/v1/invoices", url.Values{}, func(item json.RawMessage) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if page != 1 {
		t.Fatalf("expected exactly 1 page request, got %d", page)
	}
}

// TestPaginatePropagatesDoError verifica que un error HTTP de Do se propaga.
func TestPaginatePropagatesDoError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":{"type":"authentication_error","code":"invalid_api_key","message":"Clave inválida"}}`)
	})
	err := c.Paginate(context.Background(), "/v1/invoices", url.Values{}, func(item json.RawMessage) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error from Do, got nil")
	}
}
