package client

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

// StreamCrmEntityExport consumes the owner's binary once. The caller obtained
// metadata from a fresh Original Query; neither metadata nor this client grants
// permission. The server reauthorizes the native job, masks and original bytes.
func (c *Client) StreamCrmEntityExport(ctx context.Context, path, filename string, size int64, out io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return &apierr.TransportError{Err: fmt.Errorf("no se pudo preparar la descarga nativa")}
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/csv")
	if c.apiVersion != "" {
		req.Header.Set("Factuarea-Version", c.apiVersion)
	}
	hc := *c.hc
	hc.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	response, err := hc.Do(req)
	if err != nil {
		return &apierr.TransportError{Err: fmt.Errorf("descarga nativa sin completar; consulta el mismo Original")}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
		if readErr != nil {
			return &apierr.TransportError{Err: fmt.Errorf("respuesta de descarga incompleta")}
		}
		return parseError(&Response{StatusCode: response.StatusCode, Body: body, Header: response.Header,
			ContentType: response.Header.Get("Content-Type"), RequestID: response.Header.Get("X-Request-Id")})
	}
	media, params, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	disposition, attachment, dispositionErr := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
	cache := strings.ToLower(strings.ReplaceAll(response.Header.Get("Cache-Control"), " ", ""))
	if mediaErr != nil || media != "text/csv" || len(params) != 1 || !strings.EqualFold(params["charset"], "utf-8") ||
		dispositionErr != nil || disposition != "attachment" || attachment["filename"] != filename ||
		!strings.Contains(","+cache+",", ",no-store,") || !strings.Contains(","+cache+",", ",private,") ||
		!strings.EqualFold(response.Header.Get("X-Content-Type-Options"), "nosniff") ||
		size < 0 || (response.ContentLength >= 0 && response.ContentLength != size) {
		return &apierr.TransportError{Err: fmt.Errorf("la descarga no conserva la metadata original autorizada")}
	}
	if _, err := io.CopyN(out, response.Body, size); err != nil {
		return &apierr.TransportError{Err: fmt.Errorf("descarga nativa incompleta; la salida en streaming puede ser parcial")}
	}
	var extra [1]byte
	if count, err := io.ReadFull(response.Body, extra[:]); count != 0 || err != io.EOF {
		return &apierr.TransportError{Err: fmt.Errorf("la descarga no conserva el tamaño original")}
	}
	return nil
}
