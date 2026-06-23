package exit

import (
	"errors"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

func TestForError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, OK},
		{"auth", &apierr.APIError{Type: "authentication_error"}, Auth},
		{"perm", &apierr.APIError{Type: "authorization_error"}, Perm},
		{"validation", &apierr.APIError{Type: "invalid_request_error"}, Validation},
		{"notfound", &apierr.APIError{Type: "not_found_error"}, NotFound},
		{"ratelimit", &apierr.APIError{Type: "rate_limit_error"}, RateLimit},
		{"conflict", &apierr.APIError{Type: "conflict_error"}, Conflict},
		{"idempotency", &apierr.APIError{Type: "idempotency_error"}, Conflict},
		{"server", &apierr.APIError{Type: "api_error"}, Server},
		{"unavailable", &apierr.APIError{Type: "service_unavailable_error"}, Server},
		{"transport", &apierr.TransportError{Err: errors.New("dial tcp: timeout")}, Network},
		{"unknown api type", &apierr.APIError{Type: "weird"}, Server},
		{"permission", &apierr.PermissionError{Err: errors.New("falta scope")}, Perm},
		{"usage", &apierr.UsageError{Err: errors.New("x")}, Usage},
		{"generic", errors.New("boom"), CLIBug},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ForError(c.err); got != c.want {
				t.Fatalf("ForError(%v) = %d, want %d", c.err, got, c.want)
			}
		})
	}
}
