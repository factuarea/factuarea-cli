package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

type crmDeliveryRoundTrip func(*http.Request) (*http.Response, error)

func (fn crmDeliveryRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
func crmDeliveryResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestCRMDeliveryWriteTimeoutRetainsActualWireWithoutRetry(t *testing.T) {
	wire := []byte(" \n{\"amount\":999999999999999999.99,\"optional\":null,\"name\":\"Àngela 山\"}\n")
	calls := 0
	client := New("private-token", WithMaxRetries(9), WithSleep(func(time.Duration) { t.Fatal("write retry sleep") }), WithHTTPClient(&http.Client{Transport: crmDeliveryRoundTrip(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.GetBody != nil || !request.Close {
			t.Fatal("native transport could replay the idempotent write")
		}
		actual, _ := io.ReadAll(request.Body)
		if string(actual) != string(wire) || request.Header.Get("Idempotency-Key") != "first raw key" {
			t.Fatal("actual wire/key changed")
		}
		return nil, context.DeadlineExceeded
	})}))
	_, err := client.Do(context.Background(), "POST", "/v1/crm/leads", wire, map[string]string{"Idempotency-Key": "first raw key"})
	var unknown *apierr.WriteUnconfirmed
	if !errors.As(err, &unknown) {
		t.Fatalf("missing unconfirmed: %T", err)
	}
	digest := sha256.Sum256(wire)
	if calls != 1 || unknown.IdempotencyKey != "first raw key" || unknown.OriginalBodySHA256 != hex.EncodeToString(digest[:]) || unknown.Path != "/v1/crm/leads" {
		t.Fatal("lost original attempt")
	}
	if strings.Contains(unknown.Error(), "private-token") || strings.Contains(unknown.Error(), "Àngela") {
		t.Fatal("private data leaked")
	}
}

func TestCRMDeliveryHTTPFailuresAreSingleWriteAndPreserveFields(t *testing.T) {
	for _, status := range []int{403, 409, 422, 429, 500, 503, 307} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			client := New("private-bearer", WithHTTPClient(&http.Client{Transport: crmDeliveryRoundTrip(func(request *http.Request) (*http.Response, error) {
				calls++
				response := crmDeliveryResponse(status, `{"error":{"type":"owner_specific_error","message":"rejected private-bearer","details":{"field_errors":{"expected_version":["stale"]},"required_scope":"crm_leads:write"}},"errors":{"title":["required"]}}`)
				response.Header.Set("Retry-After", "13")
				response.Header.Set("Location", "/different")
				return response, nil
			})}))
			_, err := client.Do(context.Background(), "PATCH", "/v1/crm/leads/id", []byte(`{}`), nil)
			if err == nil || calls != 1 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			var unknown *apierr.WriteUnconfirmed
			if status >= 500 || status == 307 {
				if !errors.As(err, &unknown) {
					t.Fatal("ambiguous status became definitive")
				}
				return
			}
			var api *apierr.APIError
			if !errors.As(err, &api) || api.StatusCode != status || api.RetryAfter != "13" || api.Details == nil || api.Errors == nil || strings.Contains(api.Message, "private-bearer") {
				t.Fatal("typed safe error missing")
			}
		})
	}
}

func TestCRMDeliveryReadMayRetryWhileWriteCancellationBeforeDispatchIsDefinitive(t *testing.T) {
	calls := 0
	client := New("key", WithMaxRetries(1), WithSleep(func(time.Duration) {}), WithHTTPClient(&http.Client{Transport: crmDeliveryRoundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return crmDeliveryResponse(503, `{}`), nil
		}
		return crmDeliveryResponse(200, `{}`), nil
	})}))
	if _, err := client.Do(context.Background(), "GET", "/v1/crm/leads", nil, nil); err != nil || calls != 2 {
		t.Fatal("safe GET retry lost")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Do(ctx, "POST", "/v1/crm/leads", nil, nil)
	var unknown *apierr.WriteUnconfirmed
	if err == nil || errors.As(err, &unknown) || calls != 2 {
		t.Fatal("never-issued write marked ambiguous or dispatched")
	}
}

type crmDeliveryBrokenBody struct{}

func (crmDeliveryBrokenBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (crmDeliveryBrokenBody) Close() error             { return nil }
func TestCRMDeliveryIncompleteSuccessIsUnconfirmed(t *testing.T) {
	calls := 0
	client := New("key", WithHTTPClient(&http.Client{Transport: crmDeliveryRoundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		response := crmDeliveryResponse(201, "")
		response.Body = crmDeliveryBrokenBody{}
		return response, nil
	})}))
	_, err := client.Do(context.Background(), "POST", "/v1/crm/leads", []byte(`{}`), nil)
	var unknown *apierr.WriteUnconfirmed
	if !errors.As(err, &unknown) || calls != 1 {
		t.Fatal("partial success produced a receipt or retry")
	}
}

func TestCRMDeliveryRetryAfterDoesNotOverflowOrSleepNegative(t *testing.T) {
	for _, value := range []string{"-1", "999999999999999999", "3601", "invalid"} {
		t.Run(value, func(t *testing.T) {
			response := &Response{Header: http.Header{"Retry-After": {value}}}
			if got := retryDelay(response, 0); got != 200*time.Millisecond {
				t.Fatalf("unsafe delay %v", got)
			}
		})
	}
	response := &Response{Header: http.Header{"Retry-After": {"0"}}}
	if retryDelay(response, 0) != 0 {
		t.Fatal("zero delay changed")
	}
}

func TestCRMDeliveryHeaderCasePreservesActualKeyAndRejectsDuplicateIdentity(t *testing.T) {
	calls := 0
	client := New("key", WithHTTPClient(&http.Client{Transport: crmDeliveryRoundTrip(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Header.Get("Idempotency-Key") != "raw key" {
			t.Fatal("original key normalized")
		}
		return nil, context.DeadlineExceeded
	})}))
	_, err := client.Do(context.Background(), "POST", "/v1/crm/leads", []byte(`{}`), map[string]string{"idempotency-key": "raw key"})
	var unknown *apierr.WriteUnconfirmed
	if !errors.As(err, &unknown) || unknown.IdempotencyKey != "raw key" || calls != 1 {
		t.Fatal("error lost actual lowercase header identity")
	}
	_, err = client.Do(context.Background(), "POST", "/v1/crm/leads", []byte(`{}`), map[string]string{"Idempotency-Key": "first", "idempotency-key": "second"})
	if err == nil || calls != 1 {
		t.Fatal("two keys dispatched")
	}
}
