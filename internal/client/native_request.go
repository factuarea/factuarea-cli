package client

import (
	"context"
	"net/http"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

type NativeAuthRealm string

const (
	NativeStaff          NativeAuthRealm = "staff"
	NativeBookingVisitor NativeAuthRealm = "public_booking"
)

// NativeRequest retains the caller's exact bytes and original capability.
// An empty OriginalKey means the native operation has no idempotency header.
type NativeRequest struct {
	Realm       NativeAuthRealm
	Method      string
	Path        string
	Body        []byte
	OriginalKey string
	Headers     map[string]string
}

// DoNative uses the configured client, error parser and HTTP transport. The
// native original protocol owns retries and credentials; this call never
// follows redirects, replays a mutation or allocates an original key.
func (c *Client) DoNative(ctx context.Context, input NativeRequest) (*Response, error) {
	if input.Realm != NativeStaff && input.Realm != NativeBookingVisitor {
		return nil, apierr.Usagef("realm nativo inválido")
	}
	if !strings.HasPrefix(input.Path, "/") || strings.HasPrefix(input.Path, "//") {
		return nil, apierr.Usagef("ruta nativa inválida")
	}
	headers := make(map[string]string, len(input.Headers)+1)
	for name, value := range input.Headers {
		if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Idempotency-Key") {
			return nil, apierr.Usagef("la cabecera está reservada al protocolo nativo")
		}
		headers[name] = value
	}
	if input.OriginalKey != "" {
		headers["Idempotency-Key"] = input.OriginalKey
	} else {
		// Prevent Do's generic key allocator; the final transport removes this
		// internal sentinel before the configured transport sees the request.
		headers["Idempotency-Key"] = "native-no-original"
	}
	configured := *c
	WithSingleAttempt()(&configured)
	hc := *configured.hc
	transport := hc.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	hc.Transport = nativeHeaderTransport{base: transport, visitor: input.Realm == NativeBookingVisitor, original: input.OriginalKey != ""}
	configured.hc = &hc
	return configured.Do(ctx, input.Method, input.Path, input.Body, headers)
}

type nativeHeaderTransport struct {
	base     http.RoundTripper
	visitor  bool
	original bool
}

func (t nativeHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	request := req.Clone(req.Context())
	request.Header = req.Header.Clone()
	if t.visitor {
		request.Header.Del("Authorization")
	}
	if !t.original {
		request.Header.Del("Idempotency-Key")
	}
	return t.base.RoundTrip(request)
}
