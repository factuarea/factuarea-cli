// Package crmmeta_native_batch80 carries the seven native human Meta operations.
// Current native C7, published Fields, provider release and Review remain server authority.
package crmmeta_native_batch80

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

const Operation = "crm_connections.configure"
const FineScope = "crm_connections:write"
const OAuthScope = "crm_connections.write"
const prefix = "/crm/connections/meta-lead-ads"

var uuid7 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var credentialUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[47][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var externalID = regexp.MustCompile(`^[1-9][0-9]{0,31}$`)
var checksum = regexp.MustCompile(`^[0-9a-f]{64}$`)
var opaqueState = regexp.MustCompile(`^[0-9a-f-]{36}$`)
var invalidContract = errors.New("invalid closed native Meta contract")

type Action string

const (
	PrepareOAuth   Action = "prepareOAuth"
	CompleteOAuth  Action = "completeOAuth"
	Options        Action = "options"
	GetMapping     Action = "mapping"
	Inspect        Action = "inspect"
	PublishMapping Action = "publishMapping"
	PublishConsent Action = "publishConsent"
)

type Route struct {
	Method, Suffix, RouteName, OperationID string
	Mutating                               bool
}

// Routes returns a copy, preventing caller changes to native dispatch metadata.
func Routes() map[Action]Route {
	return map[Action]Route{
		PrepareOAuth:   {"POST", "/oauth/prepare", "public-api.v1.crm-meta-oauth.prepare", "prepareCrmMetaOAuth", true},
		CompleteOAuth:  {"POST", "/oauth/complete", "public-api.v1.crm-meta-oauth.complete", "completeCrmMetaOAuth", true},
		Options:        {"GET", "/options", "public-api.v1.crm-meta-options.index", "listCrmMetaConnectionOptions", false},
		GetMapping:     {"GET", "/{connection}/mappings/{form}", "public-api.v1.crm-meta-mappings.show", "getCrmMetaMapping", false},
		Inspect:        {"POST", "/{connection}/mappings/{form}/inspect", "public-api.v1.crm-meta-mappings.inspect", "inspectCrmMetaMapping", false},
		PublishMapping: {"PUT", "/{connection}/mappings/{form}", "public-api.v1.crm-meta-mappings.publish", "publishCrmMetaMapping", true},
		PublishConsent: {"POST", "/{connection}/consent", "public-api.v1.crm-meta-consent.publish", "publishCrmMetaConsent", true},
	}
}

type Mapping struct {
	RemoteName   string            `json:"remote_name"`
	Target       string            `json:"target"`
	Type         string            `json:"type"`
	FieldVersion *int64            `json:"field_version"`
	Required     bool              `json:"required"`
	Options      map[string]string `json:"options"`
}
type Grant struct {
	Stream  string   `json:"stream"`
	Actions []string `json:"actions"`
}
type PrepareInput struct {
	PageID    string `json:"page_id"`
	Confirmed bool   `json:"confirmed"`
}
type CompleteInput struct {
	State string `json:"state"`
	Code  string `json:"code"`
}
type InspectInput struct {
	CredentialUUID string `json:"credential_uuid"`
	RemoteFormID   string `json:"remote_form_id"`
}
type MappingInput struct {
	CredentialUUID             string    `json:"credential_uuid"`
	RemoteFormID               string    `json:"remote_form_id"`
	Mapping                    []Mapping `json:"mapping"`
	ExpectedMappingVersion     *int64    `json:"expected_mapping_version"`
	ExpectedPublicationVersion int64     `json:"expected_publication_version"`
	FieldsSnapshotChecksum     string    `json:"fields_snapshot_checksum"`
	Confirmed                  bool      `json:"confirmed"`
}
type ConsentInput struct {
	CredentialUUID         string  `json:"credential_uuid"`
	Grants                 []Grant `json:"grants"`
	ExpectedConsentVersion *int64  `json:"expected_consent_version"`
	Confirmed              bool    `json:"confirmed"`
}
type OAuthPreparation struct {
	AuthorizationURL string `json:"authorization_url"`
	State            string `json:"state"`
}
type OAuthInstallation struct {
	Status         string   `json:"status"`
	ConnectionUUID string   `json:"connection_uuid"`
	CredentialUUID string   `json:"credential_uuid"`
	PageID         string   `json:"page_id"`
	Scopes         []string `json:"scopes"`
}
type ConnectionOption struct {
	ConnectionUUID string  `json:"connection_uuid"`
	CredentialUUID string  `json:"credential_uuid"`
	PageID         string  `json:"page_id"`
	PageName       *string `json:"page_name"`
}
type ConnectionOptions struct {
	Options []ConnectionOption `json:"options"`
}
type MappingPublication struct {
	Status                 string    `json:"status"`
	ConnectionUUID         string    `json:"connection_uuid"`
	CredentialUUID         string    `json:"credential_uuid"`
	FormUUID               string    `json:"form_uuid"`
	PageID                 string    `json:"page_id"`
	RemoteFormID           string    `json:"remote_form_id"`
	MappingVersion         int64     `json:"mapping_version"`
	PublicationVersion     int64     `json:"publication_version"`
	FieldsSnapshotChecksum string    `json:"fields_snapshot_checksum"`
	Mapping                []Mapping `json:"mapping"`
}
type Destination struct {
	BasicFields         []string        `json:"basic_fields"`
	RequiredBasicFields []string        `json:"required_basic_fields"`
	AllowedFieldIDs     []string        `json:"allowed_field_ids"`
	RequiredFieldIDs    []string        `json:"required_field_ids"`
	FieldsSnapshot      json.RawMessage `json:"fields_snapshot"`
}
type AppReview struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}
type Inspection struct {
	Status                 string      `json:"status"`
	ConnectionUUID         string      `json:"connection_uuid"`
	CredentialUUID         string      `json:"credential_uuid"`
	FormUUID               string      `json:"form_uuid"`
	PageID                 string      `json:"page_id"`
	RemoteFormID           string      `json:"remote_form_id"`
	AppID                  string      `json:"app_id"`
	ResponsibleUUID        string      `json:"responsible_uuid"`
	RemoteFieldNames       []string    `json:"remote_field_names"`
	PublicationVersion     int64       `json:"publication_version"`
	AuthorizationVersion   int64       `json:"authorization_version"`
	ConfigurationVersion   int64       `json:"configuration_version"`
	FieldsSnapshotChecksum string      `json:"fields_snapshot_checksum"`
	ExpectedMappingVersion *int64      `json:"expected_mapping_version"`
	ExpectedConsentVersion *int64      `json:"expected_consent_version"`
	Destination            Destination `json:"destination"`
	AppReview              AppReview   `json:"app_review"`
}
type ConsentPublication struct {
	Status            string  `json:"status"`
	ConnectionUUID    string  `json:"connection_uuid"`
	AuthorizationUUID string  `json:"authorization_uuid"`
	ConsentVersion    int64   `json:"consent_version"`
	Revision          string  `json:"revision"`
	Grants            []Grant `json:"grants"`
}

// Intent retains exact first JSON wire. Its fields cannot be changed after validation.
type Intent struct {
	action                 Action
	connection, form, path string
	wire                   []byte
	body                   any
	mu                     sync.Mutex
	dispatched             bool
}

func NewIntent(action Action, connection, form string, wire []byte) (*Intent, error) {
	route, ok := Routes()[action]
	if !ok || (strings.Contains(route.Suffix, "{connection}") != (connection != "")) || (strings.Contains(route.Suffix, "{form}") != (form != "")) || (connection != "" && !uuid7.MatchString(connection)) || (form != "" && !uuid7.MatchString(form)) || len(wire) > 1048576 {
		return nil, invalidContract
	}
	i := &Intent{action: action, connection: connection, form: form, path: prefix + strings.NewReplacer("{connection}", connection, "{form}", form).Replace(route.Suffix), wire: bytes.Clone(wire)}
	if action == Options || action == GetMapping {
		if len(wire) != 0 {
			return nil, invalidContract
		}
		return i, nil
	}
	switch action {
	case PrepareOAuth:
		var b PrepareInput
		if closed(wire, &b) != nil || !externalID.MatchString(b.PageID) || !b.Confirmed {
			return nil, invalidContract
		}
		i.body = b
	case CompleteOAuth:
		var b CompleteInput
		if closed(wire, &b) != nil || !opaqueState.MatchString(b.State) || !validText(b.Code, 8192) || len(b.Code) > 8192 || b.Code == "" || strings.IndexFunc(b.Code, func(c rune) bool { return c <= 32 || c == 127 }) >= 0 {
			return nil, invalidContract
		}
		i.body = b
	case Inspect:
		var b InspectInput
		if closed(wire, &b) != nil || !credentialUUID.MatchString(b.CredentialUUID) || !externalID.MatchString(b.RemoteFormID) {
			return nil, invalidContract
		}
		i.body = b
	case PublishMapping:
		var b MappingInput
		if closed(wire, &b) != nil || !credentialUUID.MatchString(b.CredentialUUID) || !externalID.MatchString(b.RemoteFormID) || !nullableVersion(b.ExpectedMappingVersion) || !version(b.ExpectedPublicationVersion) || !checksum.MatchString(b.FieldsSnapshotChecksum) || !b.Confirmed || !validMappings(b.Mapping) {
			return nil, invalidContract
		}
		i.body = b
	case PublishConsent:
		var b ConsentInput
		if closed(wire, &b) != nil || !credentialUUID.MatchString(b.CredentialUUID) || !nullableVersion(b.ExpectedConsentVersion) || !b.Confirmed || !validGrants(b.Grants) {
			return nil, invalidContract
		}
		i.body = b
	}
	return i, nil
}
func (i *Intent) FirstWire() []byte { return bytes.Clone(i.wire) }
func (i *Intent) Action() Action    { return i.action }

// closed rejects unknown and missing fields at every structured level, including nullable CAS.
func closed(raw []byte, dst any) error {
	if !utf8.Valid(raw) || shape(raw, reflect.TypeOf(dst).Elem()) != nil {
		return invalidContract
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		return invalidContract
	}
	if d.Decode(new(any)) != io.EOF {
		return invalidContract
	}
	return nil
}

var rawMessageType = reflect.TypeOf(json.RawMessage{})

func shape(raw []byte, t reflect.Type) error {
	if t == rawMessageType {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil || obj == nil {
			return invalidContract
		}
		return nil
	}
	if t.Kind() == reflect.Pointer {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil
		}
		return shape(raw, t.Elem())
	}
	if t.Kind() == reflect.Struct {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil || obj == nil || len(obj) != t.NumField() {
			return invalidContract
		}
		for n := 0; n < t.NumField(); n++ {
			f := t.Field(n)
			v, ok := obj[f.Tag.Get("json")]
			if !ok || shape(v, f.Type) != nil {
				return invalidContract
			}
		}
	} else if t.Kind() == reflect.Slice {
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) != nil || arr == nil {
			return invalidContract
		}
		for _, v := range arr {
			if shape(v, t.Elem()) != nil {
				return invalidContract
			}
		}
	} else if t.Kind() == reflect.Map {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil || obj == nil {
			return invalidContract
		}
		for _, v := range obj {
			if shape(v, t.Elem()) != nil {
				return invalidContract
			}
		}
	} else if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return invalidContract
	}
	return nil
}
func version(v int64) bool          { return v > 0 && v < math.MaxInt64 }
func nullableVersion(v *int64) bool { return v == nil || version(*v) }
func validText(v string, max int) bool {
	if !utf8.ValidString(v) || utf8.RuneCountInString(v) > max {
		return false
	}
	for _, r := range v {
		if r <= 8 || r == 11 || r == 12 || (r >= 14 && r <= 31) || r == 127 {
			return false
		}
	}
	return true
}
func unique(v []string, ids bool) bool {
	if v == nil {
		return false
	}
	seen := map[string]bool{}
	for _, s := range v {
		if seen[s] || (ids && !uuid7.MatchString(s)) {
			return false
		}
		seen[s] = true
	}
	return true
}
func validMappings(v []Mapping) bool {
	if len(v) < 1 || len(v) > 100 {
		return false
	}
	remote, targets := map[string]bool{}, map[string]bool{}
	for _, m := range v {
		if !validText(m.RemoteName, 100) || strings.TrimSpace(m.RemoteName) == "" || remote[m.RemoteName] || targets[m.Target] || m.Options == nil {
			return false
		}
		if !strings.Contains("|text|number|date|boolean|select|multi|", "|"+m.Type+"|") {
			return false
		}
		basic := strings.Contains("|name|email|phone|mobile|company_name|notes|", "|"+m.Target+"|")
		if basic {
			if m.Type != "text" || m.FieldVersion != nil || len(m.Options) != 0 {
				return false
			}
		} else if !uuid7.MatchString(m.Target) || m.FieldVersion == nil || !version(*m.FieldVersion) {
			return false
		}
		if m.Type != "select" && m.Type != "multi" && len(m.Options) != 0 {
			return false
		}
		for name, id := range m.Options {
			if !validText(name, 4000) || strings.TrimSpace(name) == "" || !uuid7.MatchString(id) {
				return false
			}
		}
		remote[m.RemoteName] = true
		targets[m.Target] = true
	}
	return true
}
func validGrants(v []Grant) bool {
	if len(v) < 1 || len(v) > 100 {
		return false
	}
	seen := map[string]bool{}
	for _, g := range v {
		if !uuid7.MatchString(g.Stream) || seen[g.Stream] || len(g.Actions) < 1 || len(g.Actions) > 2 || !unique(g.Actions, false) {
			return false
		}
		for _, a := range g.Actions {
			if a != "read" && a != "ingest" {
				return false
			}
		}
		seen[g.Stream] = true
	}
	return true
}
func validMetaScopes(v []string) bool {
	if !unique(v, false) || len(v) > 100 {
		return false
	}
	pattern := regexp.MustCompile(`^[a-z][a-z0-9_]{0,99}$`)
	seen := map[string]bool{}
	for _, s := range v {
		if !pattern.MatchString(s) {
			return false
		}
		seen[s] = true
	}
	for _, s := range []string{"ads_management", "leads_retrieval", "pages_manage_ads", "pages_manage_metadata", "pages_read_engagement", "pages_show_list"} {
		if !seen[s] {
			return false
		}
	}
	return true
}
