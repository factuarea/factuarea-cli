package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeOwnerTransportHeadersAndExactBytes(t *testing.T) {
	for _, realm := range []NativeAuthRealm{NativeStaff, NativeBookingVisitor} {
		t.Run(string(realm), func(t *testing.T) {
			calls := 0
			wire := []byte(" {\"expected_version\":9007199254740993,\"notice_version\":\"aviso <original>\"}\n")
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, _ := io.ReadAll(r.Body)
				if string(body) != string(wire) {
					t.Error("original bytes changed")
				}
				if realm == NativeStaff && r.Header.Get("Authorization") != "Bearer staff-original-key" {
					t.Error("configured staff credential lost")
				}
				if realm == NativeBookingVisitor {
					if _, present := r.Header["Authorization"]; present {
						t.Error("visitor borrowed corporate credential")
					}
				}
				if r.Header.Get("Idempotency-Key") != "opaque-original" {
					t.Error("original key replaced")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":{}}`))
			}))
			defer s.Close()
			configured := New("staff-original-key", WithBaseURL(s.URL))
			_, err := configured.DoNative(context.Background(), NativeRequest{Realm: realm, Method: "POST", Path: "/native", Body: wire, OriginalKey: "opaque-original"})
			if err != nil || calls != 1 {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestNativeOwnerTransportControlAndManagementHaveNoOriginalHeader(t *testing.T) {
	for _, method := range []string{"GET", "PATCH"} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if _, exists := r.Header["Idempotency-Key"]; exists {
					t.Fatal("native operation allocated/sent a key")
				}
				if method == "GET" && r.Header.Get("Booking-Management-Token") != "private-token" {
					t.Fatal("native token header lost")
				}
				w.WriteHeader(204)
			}))
			defer s.Close()
			c := New("staff", WithBaseURL(s.URL))
			headers := map[string]string{}
			realm := NativeStaff
			if method == "GET" {
				headers["Booking-Management-Token"] = "private-token"
				realm = NativeBookingVisitor
			}
			_, err := c.DoNative(context.Background(), NativeRequest{Realm: realm, Method: method, Path: "/native", Headers: headers})
			if err != nil || calls != 1 {
				t.Fatalf("%d %v", calls, err)
			}
		})
	}
}

func TestNativeOwnerTransportNeverRetriesFailuresOrRedirects(t *testing.T) {
	for _, status := range []int{307, 308, 401, 409, 429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/replayed")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"type":"api_error","code":"native_unavailable","message":"No disponible"}}`))
			}))
			defer s.Close()
			c := New("staff", WithBaseURL(s.URL))
			_, err := c.DoNative(context.Background(), NativeRequest{Realm: NativeStaff, Method: "POST", Path: "/original", Body: []byte(`{}`), OriginalKey: "original"})
			if err == nil || calls != 1 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestNativeOwnerTransportDoesNotWeakenConfiguredClient(t *testing.T) {
	seen := []string{}
	transport := nativeTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r.Header.Get("Authorization"))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	httpClient := &http.Client{Transport: transport}
	c := New("original", WithHTTPClient(httpClient))
	_, err := c.DoNative(context.Background(), NativeRequest{Realm: NativeBookingVisitor, Method: "GET", Path: "/public"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Do(context.Background(), "GET", "/v1/account", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != "" || seen[1] != "Bearer original" || c.hc != httpClient || c.maxRetries != 3 {
		t.Fatalf("configured client mutated: %#v", seen)
	}
}

type nativeTestRoundTripper func(*http.Request) (*http.Response, error)

func (f nativeTestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNativeOwnerTransportRejectsCallerCredentialOverrides(t *testing.T) {
	c := New("staff")
	for _, header := range []string{"authorization", "IDEMPOTENCY-KEY"} {
		if _, err := c.DoNative(context.Background(), NativeRequest{Realm: NativeStaff, Method: "GET", Path: "/native", Headers: map[string]string{header: "forged"}}); err == nil {
			t.Fatal("credential override admitted")
		}
	}
}
