package exit

import (
	"fmt"
	"github.com/factuarea/factuarea-cli/internal/apierr"
	"testing"
)

func TestCRMDeliveryActualHTTPStatusWinsOverMissingNativeType(t *testing.T) {
	for status, expected := range map[int]int{401: Auth, 403: Perm, 404: NotFound, 409: Conflict, 422: Validation, 429: RateLimit} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			if got := ForError(&apierr.APIError{StatusCode: status, Type: "native_owner_error"}); got != expected {
				t.Fatalf("got %d expected %d", got, expected)
			}
		})
	}
}
func TestCRMDeliveryWrappedAmbiguousWriteHasDistinctExitCode(t *testing.T) {
	if got := ForError(fmt.Errorf("reported: %w", &apierr.WriteUnconfirmed{Err: &apierr.APIError{StatusCode: 503, Type: "api_error"}})); got != 11 {
		t.Fatal("unknown write treated as safe server retry")
	}
}
