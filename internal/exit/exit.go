package exit

import (
	"errors"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

const (
	OK          = 0
	CLIBug      = 1
	Usage       = 2
	Auth        = 3
	Perm        = 4
	Validation  = 5
	NotFound    = 6
	RateLimit   = 7
	Conflict    = 8
	Server      = 9
	Network     = 10
	Unconfirmed = 11
)

func ForError(err error) int {
	if err == nil {
		return OK
	}
	var unknown *apierr.WriteUnconfirmed
	if errors.As(err, &unknown) {
		return Unconfirmed
	}
	var api *apierr.APIError
	if errors.As(err, &api) {
		// The actual HTTP status wins over optional or provider-specific type names.
		switch api.StatusCode {
		case 401:
			return Auth
		case 403:
			return Perm
		case 404:
			return NotFound
		case 409:
			return Conflict
		case 422, 400:
			return Validation
		case 429:
			return RateLimit
		}
		switch api.Type {
		case "authentication_error":
			return Auth
		case "authorization_error":
			return Perm
		case "invalid_request_error":
			return Validation
		case "not_found_error":
			return NotFound
		case "rate_limit_error":
			return RateLimit
		case "conflict_error", "idempotency_error":
			return Conflict
		case "api_error", "service_unavailable_error":
			return Server
		default:
			return Server
		}
	}
	var transport *apierr.TransportError
	if errors.As(err, &transport) {
		return Network
	}
	var perm *apierr.PermissionError
	if errors.As(err, &perm) {
		return Perm
	}
	var usage *apierr.UsageError
	if errors.As(err, &usage) {
		return Usage
	}
	return CLIBug
}
