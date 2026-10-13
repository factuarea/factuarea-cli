package crmmeta_native_batch80

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	APIKey      string
	OAuthBearer string
	OAuthToken  func(context.Context) (string, error)
	BaseURL     string
	Version     string
	HTTPClient  *http.Client
}
type Client struct {
	config Config
	base   string
	http   *http.Client
}
type Response struct {
	Data      any
	RawBody   []byte
	Status    int
	RequestID string
}

// MetaError carries safe closed facts, never raw provider body/code/state/credentials.
type MetaError struct {
	Status            int
	Reason, RequestID string
	RetryAfter        *int
	Unconfirmed       bool
}

func (e *MetaError) Error() string { return "native Meta request failed: " + e.Reason }

var tokenPattern = regexp.MustCompile(`^[\x21-\x7e]+$`)
var c7Codes = map[string]bool{"crm_context_revoked": true, "crm_sandbox_blocked": true, "crm_access_denied": true, "crm_operation_unavailable": true, "insufficient_scope": true, "api_key_revoked": true}

func NewClient(config Config) (*Client, error) {
	oauth := config.OAuthToken != nil || config.OAuthBearer != ""
	if oauth {
		if config.APIKey != "" || (config.OAuthToken != nil && config.OAuthBearer != "") {
			return nil, invalidContract
		}
		if config.OAuthToken == nil && !validOAuth(config.OAuthBearer) {
			return nil, invalidContract
		}
	} else if !validAPIKey(config.APIKey) {
		return nil, invalidContract
	}
	base := config.BaseURL
	if base == "" {
		base = "https://api.factuarea.com/v1"
	}
	u, e := url.Parse(base)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, invalidContract
	}
	if config.Version != "" && !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(config.Version) {
		return nil, invalidContract
	}
	h := http.Client{Timeout: 60 * time.Second}
	if config.HTTPClient != nil {
		h = *config.HTTPClient
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport := h.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if tr, ok := transport.(*http.Transport); ok {
		owned := tr.Clone()
		owned.DisableKeepAlives = true
		h.Transport = owned
	}
	return &Client{config: config, base: strings.TrimRight(base, "/"), http: &h}, nil
}
func validOAuth(t string) bool {
	return tokenPattern.MatchString(t) && !strings.HasPrefix(t, "fact_test_") && !strings.HasPrefix(t, "fact_live_")
}
func validAPIKey(t string) bool {
	return tokenPattern.MatchString(t) && ((strings.HasPrefix(t, "fact_test_") && len(t) > 10) || (strings.HasPrefix(t, "fact_live_") && len(t) > 10))
}
func (c *Client) Execute(ctx context.Context, i *Intent) (*Response, error) {
	if i == nil {
		return nil, invalidContract
	}
	route, ok := Routes()[i.action]
	if !ok {
		return nil, invalidContract
	}
	token := c.config.APIKey
	if c.config.OAuthToken != nil {
		t, e := c.config.OAuthToken(ctx)
		if e != nil {
			return nil, &MetaError{Reason: "credential_unavailable"}
		}
		token = t
		if !validOAuth(token) {
			return nil, invalidContract
		}
	} else if c.config.OAuthBearer != "" {
		token = c.config.OAuthBearer
	}
	// NopCloser and GetBody=nil prevent net/http from reconstructing a human command for replay.
	var body io.Reader
	if len(i.wire) > 0 {
		body = io.NopCloser(bytes.NewReader(i.wire))
	}
	req, e := http.NewRequestWithContext(ctx, route.Method, c.base+i.path, body)
	if e != nil {
		return nil, invalidContract
	}
	req.GetBody = nil
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if len(i.wire) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.config.Version != "" {
		req.Header.Set("Factuarea-Version", c.config.Version)
	}
	if route.Mutating {
		i.mu.Lock()
		if i.dispatched {
			i.mu.Unlock()
			return nil, &MetaError{Reason: "already_dispatched", Unconfirmed: true}
		}
		i.dispatched = true
		i.mu.Unlock()
	}
	res, e := c.http.Do(req)
	if e != nil {
		reason := "transport_failure"
		if route.Mutating {
			reason = "unconfirmed"
		}
		return nil, &MetaError{Reason: reason, Unconfirmed: route.Mutating}
	}
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, 1048577))
	id := res.Header.Get("X-Request-Id")
	if e != nil || len(raw) > 1048576 {
		return nil, &MetaError{Status: res.StatusCode, Reason: "invalid_native_response", RequestID: id, Unconfirmed: route.Mutating}
	}
	if res.StatusCode != 200 {
		reason := "native_rejection"
		var fail struct {
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal(raw, &fail) == nil {
			var leaf string
			if json.Unmarshal(fail.Error, &leaf) == nil {
				candidate := strings.TrimPrefix(leaf, "meta_oauth_")
				if strings.Contains("|invalid_input|invalid_data|mapping_conflict|signature_invalid|permission_missing|reauth_required|rate_limited|retryable|unconfirmed|receipt_missing|unavailable|revoked|resync_required|provider_not_released|", "|"+candidate+"|") {
					reason = candidate
				}
			} else {
				var c7 struct {
					Code string `json:"code"`
				}
				if json.Unmarshal(fail.Error, &c7) == nil && c7Codes[c7.Code] {
					reason = c7.Code
				}
			}
		}
		var after *int
		if text := res.Header.Get("Retry-After"); text != "" {
			n, e := strconv.Atoi(text)
			if e == nil && n >= 0 {
				after = &n
			}
		}
		return nil, &MetaError{Status: res.StatusCode, Reason: reason, RequestID: id, RetryAfter: after, Unconfirmed: reason == "unconfirmed" || (route.Mutating && res.StatusCode >= 500 && reason == "native_rejection")}
	}
	if !strings.HasPrefix(strings.ToLower(res.Header.Get("Content-Type")), "application/json") || res.Header.Get("Location") != "" {
		return nil, &MetaError{Status: 200, Reason: "invalid_native_response", RequestID: id, Unconfirmed: route.Mutating}
	}
	data, e := projection(i, raw)
	if e != nil {
		return nil, &MetaError{Status: 200, Reason: "invalid_native_response", RequestID: id, Unconfirmed: route.Mutating}
	}
	return &Response{Data: data, RawBody: bytes.Clone(raw), Status: 200, RequestID: id}, nil
}
func projection(i *Intent, raw []byte) (any, error) {
	p := raw
	if i.action != PrepareOAuth && i.action != CompleteOAuth {
		var envelope map[string]json.RawMessage
		if json.Unmarshal(raw, &envelope) != nil || len(envelope) != 1 || envelope["data"] == nil {
			return nil, invalidContract
		}
		p = envelope["data"]
	}
	var out any
	var connection, form, credential, page, remote string
	switch i.action {
	case PrepareOAuth:
		var v OAuthPreparation
		if closed(p, &v) != nil || !opaqueState.MatchString(v.State) {
			return nil, invalidContract
		}
		u, e := url.Parse(v.AuthorizationURL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return nil, invalidContract
		}
		out = v
	case CompleteOAuth:
		var v OAuthInstallation
		if closed(p, &v) != nil || v.Status != "credentials_installed" || !uuid7.MatchString(v.ConnectionUUID) || !credentialUUID.MatchString(v.CredentialUUID) || !externalID.MatchString(v.PageID) || !validMetaScopes(v.Scopes) {
			return nil, invalidContract
		}
		out = v
	case Options:
		var v ConnectionOptions
		if closed(p, &v) != nil {
			return nil, invalidContract
		}
		for _, o := range v.Options {
			if !uuid7.MatchString(o.ConnectionUUID) || !credentialUUID.MatchString(o.CredentialUUID) || !externalID.MatchString(o.PageID) || (o.PageName != nil && !validText(*o.PageName, 4000)) {
				return nil, invalidContract
			}
		}
		out = v
	case GetMapping, PublishMapping:
		var v MappingPublication
		if closed(p, &v) != nil || v.Status != "mapping_published" || !version(v.MappingVersion) || !version(v.PublicationVersion) || !checksum.MatchString(v.FieldsSnapshotChecksum) || !validMappings(v.Mapping) {
			return nil, invalidContract
		}
		if i.action == PublishMapping {
			b := i.body.(MappingInput)
			if v.PublicationVersion != b.ExpectedPublicationVersion || v.FieldsSnapshotChecksum != b.FieldsSnapshotChecksum || !reflect.DeepEqual(v.Mapping, b.Mapping) {
				return nil, invalidContract
			}
		}
		connection, form, credential, page, remote = v.ConnectionUUID, v.FormUUID, v.CredentialUUID, v.PageID, v.RemoteFormID
		out = v
	case Inspect:
		var v Inspection
		if closed(p, &v) != nil || v.Status != "mapping_inspected" || !externalID.MatchString(v.AppID) || !credentialUUID.MatchString(v.ResponsibleUUID) || !unique(v.RemoteFieldNames, false) || !version(v.PublicationVersion) || !version(v.AuthorizationVersion) || !version(v.ConfigurationVersion) || !checksum.MatchString(v.FieldsSnapshotChecksum) || !nullableVersion(v.ExpectedMappingVersion) || !nullableVersion(v.ExpectedConsentVersion) || !unique(v.Destination.BasicFields, false) || !unique(v.Destination.RequiredBasicFields, false) || !unique(v.Destination.AllowedFieldIDs, true) || !unique(v.Destination.RequiredFieldIDs, true) || v.AppReview.Available || v.AppReview.Reason != "app_review_evidence_unavailable" {
			return nil, invalidContract
		}
		connection, form, credential, page, remote = v.ConnectionUUID, v.FormUUID, v.CredentialUUID, v.PageID, v.RemoteFormID
		out = v
	case PublishConsent:
		var v ConsentPublication
		if closed(p, &v) != nil || v.Status != "consent_published" || !credentialUUID.MatchString(v.AuthorizationUUID) || !version(v.ConsentVersion) || !validText(v.Revision, 4000) || v.Revision == "" || !validGrants(v.Grants) || !reflect.DeepEqual(v.Grants, i.body.(ConsentInput).Grants) {
			return nil, invalidContract
		}
		connection = v.ConnectionUUID
		out = v
	}
	if i.connection != "" && connection != i.connection || i.form != "" && form != i.form {
		return nil, invalidContract
	}
	if i.connection != "" && i.action != PublishConsent && (!credentialUUID.MatchString(credential) || !externalID.MatchString(page) || !externalID.MatchString(remote)) {
		return nil, invalidContract
	}
	switch b := i.body.(type) {
	case InspectInput:
		if credential != b.CredentialUUID || remote != b.RemoteFormID {
			return nil, invalidContract
		}
	case MappingInput:
		if credential != b.CredentialUUID || remote != b.RemoteFormID {
			return nil, invalidContract
		}
	}
	return out, nil
}
func (c *Client) named(ctx context.Context, a Action, connection, form string, body any) (*Response, error) {
	var wire []byte
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return nil, invalidContract
		}
		wire = b
	}
	i, e := NewIntent(a, connection, form, wire)
	if e != nil {
		return nil, e
	}
	return c.Execute(ctx, i)
}
func (c *Client) PrepareCrmMetaOAuth(ctx context.Context, b PrepareInput) (*Response, error) {
	return c.named(ctx, PrepareOAuth, "", "", b)
}
func (c *Client) CompleteCrmMetaOAuth(ctx context.Context, b CompleteInput) (*Response, error) {
	return c.named(ctx, CompleteOAuth, "", "", b)
}
func (c *Client) ListCrmMetaConnectionOptions(ctx context.Context) (*Response, error) {
	return c.named(ctx, Options, "", "", nil)
}
func (c *Client) GetCrmMetaMapping(ctx context.Context, connection, form string) (*Response, error) {
	return c.named(ctx, GetMapping, connection, form, nil)
}
func (c *Client) InspectCrmMetaMapping(ctx context.Context, connection, form string, b InspectInput) (*Response, error) {
	return c.named(ctx, Inspect, connection, form, b)
}
func (c *Client) PublishCrmMetaMapping(ctx context.Context, connection, form string, b MappingInput) (*Response, error) {
	return c.named(ctx, PublishMapping, connection, form, b)
}
func (c *Client) PublishCrmMetaConsent(ctx context.Context, connection string, b ConsentInput) (*Response, error) {
	return c.named(ctx, PublishConsent, connection, "", b)
}
