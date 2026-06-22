package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

type pageEnvelope struct {
	Data       []json.RawMessage `json:"data"`
	HasMore    bool              `json:"has_more"`
	NextCursor *string           `json:"next_cursor"`
}

// Paginate itera todas las páginas de un listado cursor, invocando each por
// objeto de data. Sigue el cursor mientras has_more sea true y next_cursor no
// esté vacío. Degrada a una sola página si la respuesta no trae has_more/
// next_cursor (catálogos enum como /v1/taxes/active devuelven {data:[...]} sin
// cursor). Propaga el error de Do o de each.
func (c *Client) Paginate(ctx context.Context, path string, query url.Values, each func(item json.RawMessage) error) error {
	for {
		u := path
		if len(query) > 0 {
			u = path + "?" + query.Encode()
		}
		resp, err := c.Do(ctx, http.MethodGet, u, nil, nil)
		if err != nil {
			return err
		}
		var env pageEnvelope
		if err := json.Unmarshal(resp.Body, &env); err != nil {
			return err
		}
		for _, item := range env.Data {
			if err := each(item); err != nil {
				return err
			}
		}
		if !env.HasMore || env.NextCursor == nil || *env.NextCursor == "" {
			return nil
		}
		query.Set("starting_after", *env.NextCursor)
	}
}
