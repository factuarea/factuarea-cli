package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var crmWhatsAppUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var crmWhatsAppDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

type crmWhatsAppTemplate struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Language   string   `json:"language"`
	Category   string   `json:"category"`
	Parameters []string `json:"parameters"`
}
type crmWhatsAppAddress struct {
	Channel string  `json:"channel"`
	Phone   string  `json:"phone"`
	Name    *string `json:"name"`
}
type crmWhatsAppRecipient struct {
	Channel string  `json:"channel"`
	Phone   string  `json:"phone"`
	Name    *string `json:"name"`
	Kind    string  `json:"kind"`
}
type crmWhatsAppPreparation struct {
	ConversationID       string               `json:"conversation_id"`
	MessageID            string               `json:"message_id"`
	EffectID             string               `json:"effect_id"`
	ExpectedVersion      int64                `json:"expected_version"`
	MessageVersion       int64                `json:"message_version"`
	Purpose              string               `json:"purpose"`
	Sender               crmWhatsAppAddress   `json:"sender"`
	Recipient            crmWhatsAppRecipient `json:"recipient"`
	Body                 string               `json:"body"`
	Template             *crmWhatsAppTemplate `json:"template"`
	PreparationToken     string               `json:"preparation_token"`
	SnapshotDigest       string               `json:"snapshot_digest"`
	AuthorizationVersion string               `json:"authorization_version"`
}
type crmWhatsAppSendBody struct {
	ConversationID   string               `json:"conversation_id"`
	EffectID         string               `json:"effect_id"`
	ExpectedVersion  int64                `json:"expected_version"`
	MessageVersion   int64                `json:"message_version"`
	Purpose          string               `json:"purpose"`
	Template         *crmWhatsAppTemplate `json:"template"`
	PreparationToken string               `json:"preparation_token"`
}
type crmWhatsAppReceipt struct {
	Status            string  `json:"status"`
	ProviderMessageID *string `json:"provider_message_id"`
	SubmittedAt       *string `json:"submitted_at"`
	ReasonCode        *string `json:"reason_code"`
}
type crmWhatsAppOriginal struct {
	State             string              `json:"state"`
	ConversationID    string              `json:"conversation_id"`
	MessageID         string              `json:"message_id"`
	EffectID          string              `json:"effect_id"`
	OriginalWireHash  string              `json:"original_wire_hash"`
	PreparationDigest string              `json:"preparation_digest"`
	Receipt           *crmWhatsAppReceipt `json:"receipt"`
}
type crmWhatsAppIntent struct {
	Format             string                 `json:"format"`
	Preparation        crmWhatsAppPreparation `json:"preparation"`
	FirstWire          string                 `json:"first_wire"`
	WireHash           string                 `json:"wire_hash"`
	ContextFingerprint string                 `json:"context_fingerprint"`
	Dispatched         bool                   `json:"dispatched"`
}

func registerCRMWhatsAppCommands(root *cobra.Command) {
	var crm *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "crm" {
			crm = c
			break
		}
	}
	if crm == nil {
		crm = serviceLevelGroup("crm", "Comandos CRM")
		root.AddCommand(crm)
	}
	group := serviceLevelGroup("whatsapp", "Preparación nativa, envío confirmado y GET original con API key real")
	group.AddCommand(newCRMWhatsAppPrepareCommand(), newCRMWhatsAppSendCommand(), newCRMWhatsAppOriginalCommand())
	crm.AddCommand(group)
}
func newCRMWhatsAppPrepareCommand() *cobra.Command {
	var intentFile, templateFile, firstFile, purpose, version string
	var revalidate bool
	c := &cobra.Command{Use: "prepare conversation message", Short: "Prepara un efecto nativo sin enviar (conversations:write)", Args: UsageArgs(cobra.ExactArgs(2))}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if intentFile == "" || !crmWhatsAppUUID.MatchString(args[0]) || !crmWhatsAppUUID.MatchString(args[1]) || (purpose != "contact_request" && purpose != "marketing") {
			return apierr.Usagef("requiere UUIDv7 conversation/message, --purpose y --intent-file nuevo")
		}
		if revalidate {
			if templateFile != "" || firstFile != "" {
				return apierr.Usagef("revalidate conserva el template y los bytes del checkpoint sin reemplazos")
			}
			intent, err := crmWhatsAppLoad(intentFile)
			if err != nil {
				return err
			}
			p := intent.Preparation
			if strings.ToLower(args[0]) != p.ConversationID || strings.ToLower(args[1]) != p.MessageID || purpose != p.Purpose {
				return apierr.Usagef("revalidate requiere los mismos IDs y propósito originales")
			}
			query := url.Values{"purpose": {p.Purpose}, "preparation_token": {p.PreparationToken}}
			if p.Template != nil {
				raw, _ := json.Marshal(p.Template)
				query.Set("template", string(raw))
			}
			wire, err := crmWhatsAppDispatch(cmd, "GET", crmWhatsAppPath(p.ConversationID, p.MessageID, "preparation"), query, "", version, intent.ContextFingerprint, false)
			if err != nil {
				return err
			}
			var envelope struct {
				Data crmWhatsAppPreparation `json:"data"`
			}
			if json.Unmarshal(wire, &envelope) != nil {
				return crmOAuthContractError()
			}
			before, _ := json.Marshal(p)
			after, _ := json.Marshal(envelope.Data)
			if !bytes.Equal(before, after) {
				return crmOAuthContractError()
			}
			return crmWhatsAppOutput(cmd, wire, "prepared", p.EffectID)
		}
		if _, err := os.Lstat(intentFile); !os.IsNotExist(err) {
			return apierr.Usagef("intent-file debe ser nuevo; conserve el efecto original existente")
		}
		conversation, message := strings.ToLower(args[0]), strings.ToLower(args[1])
		query := url.Values{"purpose": {purpose}}
		var selected *crmWhatsAppTemplate
		if templateFile != "" {
			raw, err := os.ReadFile(templateFile)
			if err != nil {
				return apierr.ReadFileUsagef(templateFile, err)
			}
			selected = &crmWhatsAppTemplate{}
			if crmWhatsAppDecode(raw, selected) != nil || !crmWhatsAppValidTemplate(selected) {
				return apierr.Usagef("template-file requiere id/name/language/category/parameters nativos")
			}
			query.Set("template", string(raw))
		}
		fingerprint, err := crmWhatsAppContext(cmd, version)
		if err != nil {
			return err
		}
		wire, err := crmWhatsAppDispatch(cmd, "GET", crmWhatsAppPath(conversation, message, "preparation"), query, "", version, fingerprint, false)
		if err != nil {
			return err
		}
		var envelope struct {
			Data crmWhatsAppPreparation `json:"data"`
		}
		if !crmWhatsAppDataFields(wire, []string{"conversation_id", "message_id", "effect_id", "expected_version", "message_version", "purpose", "sender", "recipient", "body", "template", "preparation_token", "snapshot_digest", "authorization_version"}) || json.Unmarshal(wire, &envelope) != nil || !crmWhatsAppValidPreparation(envelope.Data, conversation, message) || envelope.Data.Purpose != purpose || !crmWhatsAppTemplateEqual(envelope.Data.Template, selected) {
			return crmOAuthContractError()
		}
		p := envelope.Data
		body := crmWhatsAppBody(p)
		raw, err := json.Marshal(body)
		if err != nil {
			return crmOAuthContractError()
		}
		if firstFile != "" {
			raw, err = os.ReadFile(firstFile)
			if err != nil {
				return apierr.ReadFileUsagef(firstFile, err)
			}
			if !crmWhatsAppValidWire(raw, p) {
				return apierr.Usagef("first-body-file debe conservar los siete campos exactos de esta preparación")
			}
		}
		intent := crmWhatsAppIntent{Format: "factuarea.crm.whatsapp.v1", Preparation: p, FirstWire: string(raw), WireHash: crmWhatsAppHash(raw), ContextFingerprint: fingerprint}
		if err := crmWhatsAppSaveNew(intentFile, intent); err != nil {
			return err
		}
		return crmWhatsAppOutput(cmd, wire, "prepared", p.EffectID)
	}
	c.Flags().StringVar(&intentFile, "intent-file", "", "checkpoint privado nuevo (0600), conserva wire/efecto/contexto")
	c.Flags().BoolVar(&revalidate, "revalidate", false, "GET del mismo token/efecto del checkpoint existente; no crea otro intento ni habilita replay")
	c.Flags().StringVar(&purpose, "purpose", "", "contact_request o marketing")
	c.Flags().StringVar(&templateFile, "template-file", "", "selección de plantilla aprobada que validará el servidor")
	c.Flags().StringVar(&firstFile, "first-body-file", "", "wire opcional exacto de siete campos; se valida contra la preparación")
	c.Flags().StringVar(&version, "api-version", "", "Factuarea-Version (YYYY-MM-DD)")
	return c
}
func newCRMWhatsAppSendCommand() *cobra.Command {
	var intentFile, confirm, version string
	c := &cobra.Command{Use: "send", Short: "Envía una sola vez el efecto preparado (conversations:write)", Args: UsageArgs(cobra.NoArgs)}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		if intentFile == "" {
			return apierr.Usagef("requiere --intent-file y --confirm message UUID")
		}
		lock := intentFile + ".lock"
		if err := os.Mkdir(lock, 0700); err != nil {
			return apierr.Usagef("el intent está ocupado; no se ha enviado")
		}
		defer os.Remove(lock)
		intent, err := crmWhatsAppLoad(intentFile)
		if err != nil {
			return err
		}
		if intent.Dispatched {
			return apierr.Usagef("efecto ya despachado; consulte crm whatsapp original, sin repetir SEND")
		}
		if confirm != intent.Preparation.MessageID {
			return apierr.Usagef("--confirm debe ser el UUID exacto del message preparado")
		}
		cc, err := newCLIContext(globalsFrom(cmd), "")
		if err != nil {
			return err
		}
		if err := safety.RequireLive(cc.res.Environment, cc.g.Live); err != nil {
			return err
		}
		fingerprint, err := crmWhatsAppContext(cmd, version)
		if err != nil {
			return err
		}
		if fingerprint != intent.ContextFingerprint {
			return apierr.Usagef("el contexto original de credencial/servidor/versión cambió")
		}
		intent.Dispatched = true
		if err := crmWhatsAppSaveExisting(intentFile, intent); err != nil {
			return err
		}
		p := intent.Preparation
		wire, err := crmWhatsAppDispatch(cmd, "POST", crmWhatsAppPath(p.ConversationID, p.MessageID, "send"), nil, intent.FirstWire, version, fingerprint, true)
		if err != nil {
			return err
		}
		var envelope struct {
			Data crmWhatsAppReceipt `json:"data"`
		}
		if !crmWhatsAppDataFields(wire, []string{"status", "provider_message_id", "submitted_at", "reason_code"}) || json.Unmarshal(wire, &envelope) != nil || !crmWhatsAppValidReceipt(envelope.Data) {
			return crmWhatsAppUnconfirmed()
		}
		return crmWhatsAppOutput(cmd, wire, envelope.Data.Status, p.EffectID)
	}
	c.Flags().StringVar(&intentFile, "intent-file", "", "checkpoint original (0600)")
	c.Flags().StringVar(&confirm, "confirm", "", "UUID exacto del message preparado; consentimiento explícito para SEND")
	c.Flags().StringVar(&version, "api-version", "", "misma Factuarea-Version del checkpoint")
	return c
}
func newCRMWhatsAppOriginalCommand() *cobra.Command {
	var intentFile, version string
	c := &cobra.Command{Use: "original", Short: "GET original_wire exacto; conserva el efecto y no reenvía (conversations:read)", Args: UsageArgs(cobra.NoArgs)}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		intent, err := crmWhatsAppLoad(intentFile)
		if err != nil {
			return err
		}
		p := intent.Preparation
		wire, err := crmWhatsAppDispatch(cmd, "GET", crmWhatsAppPath(p.ConversationID, p.MessageID, "original"), url.Values{"original_wire": {intent.FirstWire}}, "", version, intent.ContextFingerprint, false)
		if err != nil {
			return err
		}
		var envelope struct {
			Data crmWhatsAppOriginal `json:"data"`
		}
		if !crmWhatsAppDataFields(wire, []string{"state", "conversation_id", "message_id", "effect_id", "original_wire_hash", "preparation_digest", "receipt"}) || json.Unmarshal(wire, &envelope) != nil {
			return crmOAuthContractError()
		}
		r := envelope.Data
		if r.ConversationID != p.ConversationID || r.MessageID != p.MessageID || r.EffectID != p.EffectID || r.OriginalWireHash != intent.WireHash || r.PreparationDigest != p.SnapshotDigest {
			return crmOAuthContractError()
		}
		if r.State == "not_found" {
			if r.Receipt != nil {
				return crmOAuthContractError()
			}
		} else if r.Receipt == nil || !crmWhatsAppValidReceipt(*r.Receipt) || r.Receipt.Status != r.State {
			return crmOAuthContractError()
		}
		return crmWhatsAppOutput(cmd, wire, r.State, p.EffectID)
	}
	c.Flags().StringVar(&intentFile, "intent-file", "", "checkpoint original exacto (0600)")
	c.Flags().StringVar(&version, "api-version", "", "misma Factuarea-Version del checkpoint")
	return c
}
func crmWhatsAppPath(c, m, a string) string {
	return "/v1/crm/conversation/" + c + "/messages/" + m + "/whatsapp-cloud/" + a
}
func crmWhatsAppDataFields(raw []byte, expected []string) bool {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(envelope["data"], &fields) != nil || len(fields) != len(expected) {
		return false
	}
	for _, key := range expected {
		if fields[key] == nil {
			return false
		}
	}
	return true
}
func crmWhatsAppHash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func crmWhatsAppDecode(raw []byte, value any) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("invalid UTF8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("extra JSON")
	}
	return nil
}
func crmWhatsAppValidTemplate(t *crmWhatsAppTemplate) bool {
	if t == nil {
		return true
	}
	if !regexp.MustCompile(`^[1-9][0-9]{0,63}$`).MatchString(t.ID) || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,511}$`).MatchString(t.Name) || !regexp.MustCompile(`^[a-z]{2,3}(?:_[A-Z]{2})?$`).MatchString(t.Language) || t.Parameters == nil || len(t.Parameters) > 100 {
		return false
	}
	for _, p := range t.Parameters {
		if !crmWhatsAppNativeText(p, 4096) {
			return false
		}
	}
	return t.Category == "MARKETING" || t.Category == "UTILITY" || t.Category == "AUTHENTICATION"
}
func crmWhatsAppNativeText(v string, maximum int) bool {
	return utf8.ValidString(v) && strings.TrimSpace(v) != "" && utf8.RuneCountInString(v) <= maximum && !regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`).MatchString(v)
}
func crmWhatsAppTemplateEqual(a, b *crmWhatsAppTemplate) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
func crmWhatsAppValidPreparation(p crmWhatsAppPreparation, c, m string) bool {
	phone := regexp.MustCompile(`^\+[1-9][0-9]{5,14}$`)
	return p.ConversationID == c && p.MessageID == m && crmWhatsAppUUID.MatchString(p.EffectID) && p.ExpectedVersion > 0 && p.MessageVersion > 0 && (p.Purpose == "contact_request" || p.Purpose == "marketing") && p.Sender.Channel == "whatsapp" && p.Recipient.Channel == "whatsapp" && p.Recipient.Kind == "to" && phone.MatchString(p.Sender.Phone) && phone.MatchString(p.Recipient.Phone) && p.Body != "" && utf8.RuneCountInString(p.Body) <= 4096 && p.PreparationToken != "" && len(p.PreparationToken) <= 65536 && crmWhatsAppDigest.MatchString(p.SnapshotDigest) && p.AuthorizationVersion != "" && len(p.AuthorizationVersion) <= 255 && crmWhatsAppValidTemplate(p.Template)
}
func crmWhatsAppBody(p crmWhatsAppPreparation) crmWhatsAppSendBody {
	return crmWhatsAppSendBody{p.ConversationID, p.EffectID, p.ExpectedVersion, p.MessageVersion, p.Purpose, p.Template, p.PreparationToken}
}
func crmWhatsAppValidWire(raw []byte, p crmWhatsAppPreparation) bool {
	if len(raw) == 0 || len(raw) > 65536 || !utf8.Valid(raw) {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 7 {
		return false
	}
	for _, key := range []string{"conversation_id", "effect_id", "expected_version", "message_version", "purpose", "template", "preparation_token"} {
		if fields[key] == nil {
			return false
		}
	}
	var body crmWhatsAppSendBody
	if crmWhatsAppDecode(raw, &body) != nil {
		return false
	}
	expected := crmWhatsAppBody(p)
	a, _ := json.Marshal(body)
	b, _ := json.Marshal(expected)
	return bytes.Equal(a, b)
}
func crmWhatsAppValidReceipt(r crmWhatsAppReceipt) bool {
	if r.Status != "confirmed" && r.Status != "unconfirmed" && r.Status != "failed" {
		return false
	}
	if r.ProviderMessageID != nil && !crmWhatsAppNativeText(*r.ProviderMessageID, 1024) {
		return false
	}
	if r.SubmittedAt != nil {
		d, e := time.Parse("2006-01-02T15:04:05Z", *r.SubmittedAt)
		if e != nil || d.Format("2006-01-02T15:04:05Z") != *r.SubmittedAt {
			return false
		}
	}
	if r.ReasonCode != nil && !regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`).MatchString(*r.ReasonCode) {
		return false
	}
	if r.Status == "confirmed" && r.SubmittedAt == nil {
		return false
	}
	if r.Status == "failed" && (r.ProviderMessageID != nil || r.SubmittedAt != nil || r.ReasonCode == nil) {
		return false
	}
	return true
}
func crmWhatsAppContext(cmd *cobra.Command, version string) (string, error) {
	if err := validateCRMDocumentHeaders(version, ""); err != nil {
		return "", err
	}
	cc, err := newCLIContext(globalsFrom(cmd), "")
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(cc.res.APIKey, "fact_test_") && !strings.HasPrefix(cc.res.APIKey, "fact_live_") {
		return "", apierr.Usagef("WhatsApp requiere una API key real; el catálogo no emite grants OAuth conversación")
	}
	base := os.Getenv(envBaseURL)
	if base == "" {
		base = "https://api.factuarea.com"
	}
	if err := validateBaseURLTransport(base, globalsFrom(cmd).AllowInsecureTransport); err != nil {
		return "", err
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", apierr.Usagef("servidor HTTP nativo inválido")
	}
	raw, _ := json.Marshal([]string{strings.TrimRight(base, "/"), cc.res.APIKey, version})
	return crmWhatsAppHash(raw), nil
}
func crmWhatsAppDispatch(cmd *cobra.Command, method, path string, query url.Values, body, version, fingerprint string, mutation bool) ([]byte, error) {
	current, err := crmWhatsAppContext(cmd, version)
	if err != nil {
		return nil, err
	}
	if current != fingerprint {
		return nil, apierr.Usagef("el contexto original de credencial/servidor/versión cambió")
	}
	cc, err := newCLIContext(globalsFrom(cmd), "")
	if err != nil {
		return nil, err
	}
	base := os.Getenv(envBaseURL)
	if base == "" {
		base = "https://api.factuarea.com"
	}
	endpoint := strings.TrimRight(base, "/") + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(cmd.Context(), method, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, crmOAuthContractError()
	}
	req.GetBody = nil
	req.Header.Set("Authorization", "Bearer "+cc.res.APIKey)
	req.Header.Set("Accept", "application/json")
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}
	if version != "" {
		req.Header.Set("Factuarea-Version", version)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 60 * time.Second, Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		if mutation {
			return nil, crmWhatsAppUnconfirmed()
		}
		return nil, &apierr.TransportError{Err: fmt.Errorf("no se pudo leer el WhatsApp nativo; no se ha enviado ni repetido SEND")}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		if mutation {
			return nil, crmWhatsAppUnconfirmed()
		}
		return nil, crmOAuthContractError()
	}
	if response.StatusCode != 200 {
		if mutation && response.StatusCode >= 500 {
			return nil, crmWhatsAppUnconfirmed(response.Header.Get("X-Request-Id"))
		}
		var payload struct {
			Error     struct{ Type, Code, Message, Subcode string }
			RequestID string `json:"request_id"`
		}
		_ = json.Unmarshal(raw, &payload)
		if payload.Error.Message == "" {
			payload.Error.Message = "WhatsApp nativo rechazado"
		}
		if payload.RequestID == "" {
			payload.RequestID = response.Header.Get("X-Request-Id")
		}
		return nil, &apierr.APIError{StatusCode: response.StatusCode, Type: payload.Error.Type, Code: payload.Error.Code, Message: payload.Error.Message, Subcode: payload.Error.Subcode, RequestID: payload.RequestID}
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "application/json") || response.Header.Get("Location") != "" {
		if mutation {
			return nil, crmWhatsAppUnconfirmed()
		}
		return nil, crmOAuthContractError()
	}
	return raw, nil
}

type crmWhatsAppSendUnconfirmedError struct{ RequestID string }

func (e *crmWhatsAppSendUnconfirmedError) Error() string {
	return "SEND WhatsApp sin confirmación; conserve el checkpoint y consulte crm whatsapp original; no se ha repetido"
}
func (e *crmWhatsAppSendUnconfirmedError) Unwrap() error {
	return &apierr.TransportError{Err: fmt.Errorf("%s", e.Error())}
}
func crmWhatsAppUnconfirmed(requestIDs ...string) error {
	id := ""
	if len(requestIDs) > 0 {
		id = requestIDs[0]
	}
	return &crmWhatsAppSendUnconfirmedError{RequestID: id}
}
func crmWhatsAppSaveNew(path string, intent crmWhatsAppIntent) error {
	raw, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return crmOAuthContractError()
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return apierr.Usagef("no se pudo crear el checkpoint privado nuevo")
	}
	defer file.Close()
	if _, err = file.Write(append(raw, '\n')); err == nil {
		err = file.Sync()
	}
	if err != nil {
		return apierr.Usagef("no se pudo guardar el checkpoint privado")
	}
	return nil
}
func crmWhatsAppSaveExisting(path string, intent crmWhatsAppIntent) error {
	raw, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return crmOAuthContractError()
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".whatsapp-intent-*")
	if err != nil {
		return apierr.Usagef("no se pudo guardar la marca de dispatch")
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(append(raw, '\n'))
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return apierr.Usagef("no se pudo guardar la marca de dispatch")
	}
	if err = os.Rename(name, path); err != nil {
		return apierr.Usagef("no se pudo guardar la marca de dispatch")
	}
	return nil
}
func crmWhatsAppLoad(path string) (crmWhatsAppIntent, error) {
	var intent crmWhatsAppIntent
	stat, err := os.Lstat(path)
	if err != nil || !stat.Mode().IsRegular() || stat.Mode().Perm() != 0600 {
		return intent, apierr.Usagef("intent-file requiere un checkpoint regular privado 0600")
	}
	raw, err := os.ReadFile(path)
	if err != nil || crmWhatsAppDecode(raw, &intent) != nil || intent.Format != "factuarea.crm.whatsapp.v1" || !crmWhatsAppValidPreparation(intent.Preparation, intent.Preparation.ConversationID, intent.Preparation.MessageID) || !crmWhatsAppUUID.MatchString(intent.Preparation.ConversationID) || !crmWhatsAppUUID.MatchString(intent.Preparation.MessageID) || !crmWhatsAppDigest.MatchString(intent.ContextFingerprint) || !crmWhatsAppValidWire([]byte(intent.FirstWire), intent.Preparation) || intent.WireHash != crmWhatsAppHash([]byte(intent.FirstWire)) {
		return intent, apierr.Usagef("checkpoint WhatsApp original inválido; no se ha enviado")
	}
	return intent, nil
}
func crmWhatsAppOutput(cmd *cobra.Command, wire []byte, state, effect string) error {
	if globalsFrom(cmd).JSON {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), string(wire))
		return err
	}
	table := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintf(table, "ESTADO\tEFECTO\n%s\t%s\n", state, effect); err != nil {
		return err
	}
	return table.Flush()
}
func crmWhatsAppManifest() []manifestEntry {
	result := []manifestEntry{}
	for i, c := range []*cobra.Command{newCRMWhatsAppPrepareCommand(), newCRMWhatsAppSendCommand(), newCRMWhatsAppOriginalCommand()} {
		operation, scope := "conversations.send", "conversations:write"
		if i == 2 {
			operation, scope = "conversations.get", "conversations:read"
		}
		entry := manifestEntry{Command: "factuarea crm whatsapp " + c.Name(), OperationID: operation, RequiredScope: scope, Summary: c.Short, Args: []string{}, Flags: []flagInfo{}, Mutating: i == 1, Irreversible: i == 1}
		if i == 0 {
			entry.Args = []string{"conversation", "message"}
		}
		c.Flags().VisitAll(func(flag *pflag.Flag) { entry.Flags = append(entry.Flags, flagInfo{flag.Name, flag.Value.Type()}) })
		result = append(result, entry)
	}
	return result
}
