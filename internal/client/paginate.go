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
