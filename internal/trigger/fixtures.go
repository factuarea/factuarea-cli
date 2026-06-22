package trigger

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/factuarea/factuarea-cli/internal/client"
)

func init() {
	registry["client.created"] = func(ctx context.Context, c *client.Client, ov map[string]string) error {
		_, err := c.Do(ctx, http.MethodPost, "/v1/clients", mustJSON(map[string]any{
			"name":   orDefault(ov, "name", "Cliente de prueba (trigger)"),
			"tax_id": orDefault(ov, "tax_id", "12345678Z"),
		}), nil)
		return err
	}

	registry["product.created"] = func(ctx context.Context, c *client.Client, ov map[string]string) error {
		_, err := c.Do(ctx, http.MethodPost, "/v1/products", mustJSON(map[string]any{
			"name":  orDefault(ov, "name", "Producto de prueba (trigger)"),
			"price": 100,
		}), nil)
		return err
	}

	registry["invoice.created"] = func(ctx context.Context, c *client.Client, ov map[string]string) error {
		_, err := createInvoice(ctx, c, ov)
		return err
	}

	registry["invoice.sent"] = func(ctx context.Context, c *client.Client, ov map[string]string) error {
		id, err := createInvoice(ctx, c, ov)
		if err != nil {
			return err
		}
		_, err = c.Do(ctx, http.MethodPost, "/v1/invoices/"+id+"/send", nil, nil)
		return err
	}

	registry["invoice.paid"] = func(ctx context.Context, c *client.Client, ov map[string]string) error {
		id, err := createInvoice(ctx, c, ov)
		if err != nil {
			return err
		}
		_, err = c.Do(ctx, http.MethodPost, "/v1/invoices/"+id+"/mark-paid", nil, nil)
		return err
	}

	registry["quote.created"] = func(ctx context.Context, c *client.Client, ov map[string]string) error {
		_, err := createQuote(ctx, c, ov)
		return err
	}

	registry["quote.approved"] = func(ctx context.Context, c *client.Client, ov map[string]string) error {
		id, err := createQuote(ctx, c, ov)
		if err != nil {
			return err
		}
		_, err = c.Do(ctx, http.MethodPost, "/v1/quotes/"+id+"/accept", nil, nil)
		return err
	}
}

func createInvoice(ctx context.Context, c *client.Client, ov map[string]string) (string, error) {
	clientID, err := ensureClientID(ctx, c)
	if err != nil {
		return "", err
	}
	seriesID, err := defaultSeriesID(ctx, c)
	if err != nil {
		return "", err
	}
	taxID, err := defaultTaxID(ctx, c)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(ctx, http.MethodPost, "/v1/invoices", mustJSON(map[string]any{
		"client_id": clientID,
		"series_id": seriesID,
		"issued_on": time.Now().Format("2006-01-02"),
		"lines": []map[string]any{{
			"description": "Servicio de prueba (trigger)",
			"quantity":    1,
			"unit_price":  100,
			"tax_rate_id": taxID,
		}},
	}), nil)
	if err != nil {
		return "", err
	}
	return extractID(resp.Body)
}

func createQuote(ctx context.Context, c *client.Client, ov map[string]string) (string, error) {
	clientID, err := ensureClientID(ctx, c)
	if err != nil {
		return "", err
	}
	seriesID, err := defaultSeriesID(ctx, c)
	if err != nil {
		return "", err
	}
	taxID, err := defaultTaxID(ctx, c)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(ctx, http.MethodPost, "/v1/quotes", mustJSON(map[string]any{
		"client_id": clientID,
		"series_id": seriesID,
		"issued_on": time.Now().Format("2006-01-02"),
		"lines": []map[string]any{{
			"description": "Servicio de prueba (trigger)",
			"quantity":    1,
			"unit_price":  100,
			"tax_rate_id": taxID,
		}},
	}), nil)
	if err != nil {
		return "", err
	}
	return extractID(resp.Body)
}

func ensureClientID(ctx context.Context, c *client.Client) (string, error) {
	resp, err := c.Do(ctx, http.MethodGet, "/v1/clients?"+url.Values{"limit": {"1"}}.Encode(), nil, nil)
	if err != nil {
		return "", err
	}
	if id := firstListID(resp.Body); id != "" {
		return id, nil
	}
	created, err := c.Do(ctx, http.MethodPost, "/v1/clients", mustJSON(map[string]any{
		"name":   "Cliente de prueba (trigger)",
		"tax_id": "12345678Z",
	}), nil)
	if err != nil {
		return "", err
	}
	return extractID(created.Body)
}

func defaultSeriesID(ctx context.Context, c *client.Client) (string, error) {
	resp, err := c.Do(ctx, http.MethodGet, "/v1/series?"+url.Values{"limit": {"100"}}.Encode(), nil, nil)
	if err != nil {
		return "", err
	}
	items, err := listItems(resp.Body)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", fmt.Errorf("no hay ninguna serie en la cuenta sandbox; crea una serie antes de disparar este evento")
	}
	for _, it := range items {
		if it.IsDefault {
			return it.ID, nil
		}
	}
	return items[0].ID, nil
}

func defaultTaxID(ctx context.Context, c *client.Client) (string, error) {
	resp, err := c.Do(ctx, http.MethodGet, "/v1/taxes/active?"+url.Values{"limit": {"1"}}.Encode(), nil, nil)
	if err != nil {
		return "", err
	}
	if id := firstListID(resp.Body); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("no hay ningún impuesto activo en la cuenta sandbox; configura un impuesto antes de disparar este evento")
}

type listItem struct {
	ID        string `json:"id"`
	IsDefault bool   `json:"is_default"`
}

func listItems(body []byte) ([]listItem, error) {
	var env struct {
		Data []listItem `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	return env.Data, nil
}

func firstListID(body []byte) string {
	items, err := listItems(body)
	if err != nil || len(items) == 0 {
		return ""
	}
	return items[0].ID
}

func extractID(body []byte) (string, error) {
	var env struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return "", err
	}
	if env.Data.ID == "" {
		return "", fmt.Errorf("la respuesta no contiene un id de recurso: %s", string(body))
	}
	return env.Data.ID, nil
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func orDefault(ov map[string]string, key, def string) string {
	if v, ok := ov[key]; ok && v != "" {
		return v
	}
	return def
}
