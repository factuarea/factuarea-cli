package cmd

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

// This private local checkpoint retains the original wire bytes. It is not a
// server receipt, effect log, credential store or confirmation of an effect.
type crmDocumentIntent struct {
	Schema                int      `json:"schema"`
	Group                 string   `json:"group"`
	Action                string   `json:"action"`
	Method                string   `json:"method"`
	Path                  string   `json:"path"`
	Args                  []string `json:"args"`
	Body                  string   `json:"body"`
	Key                   string   `json:"idempotency_key"`
	BaseURL               string   `json:"base_url"`
	Profile               string   `json:"profile"`
	Environment           string   `json:"environment"`
	CredentialFingerprint string   `json:"credential_fingerprint"`
	APIVersion            string   `json:"api_version"`
	ActiveProfile         string   `json:"active_profile"`
	Seal                  string   `json:"seal"`
}

func crmDocumentBaseURL() string {
	base := os.Getenv(envBaseURL)
	if base == "" {
		base = "https://api.factuarea.com"
	}
	return strings.TrimRight(base, "/")
}

func crmDocumentAuthFingerprint(key string) string {
	h := sha256.Sum256([]byte("crm-document-auth\x00" + key))
	return hex.EncodeToString(h[:])
}

func (intent crmDocumentIntent) signature(key string) string {
	intent.Seal = ""
	body, _ := json.Marshal(intent)
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte("factuarea-cli-crm-document-intent-v1\x00"))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func newCRMDocumentIntent(op crmDocumentOperation, args []string, body []byte, key, apiVersion, activeProfile string, cc *cliContext) crmDocumentIntent {
	intent := crmDocumentIntent{Schema: 1, Group: op.group, Action: op.action, Method: op.method, Path: op.pathFor(args),
		Args: append([]string{}, args...), Body: string(body), Key: key, BaseURL: crmDocumentBaseURL(),
		Profile: cc.res.Profile, Environment: cc.res.Environment, CredentialFingerprint: crmDocumentAuthFingerprint(cc.res.APIKey),
		APIVersion: apiVersion, ActiveProfile: activeProfile}
	intent.Seal = intent.signature(cc.res.APIKey)
	return intent
}

func crmDocumentWritePrivate(path string, body []byte) error {
	if path == "" || path == "-" {
		return apierr.Usagef("declara un fichero privado con --intent-file; no se admite stdout")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return apierr.WriteFileUsagef(path, err)
	}
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return apierr.WriteFileUsagef(path, err)
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return apierr.WriteFileUsagef(path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return apierr.WriteFileUsagef(path, err)
	}
	if err := f.Close(); err != nil {
		return apierr.WriteFileUsagef(path, err)
	}
	return nil
}

func saveCRMDocumentIntent(path string, intent crmDocumentIntent) error {
	body, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return err
	}
	return crmDocumentWritePrivate(path, append(body, '\n'))
}

func loadCRMDocumentIntent(path string) (crmDocumentIntent, error) {
	var intent crmDocumentIntent
	before, err := os.Lstat(path)
	if err != nil {
		return intent, apierr.ReadFileUsagef(path, err)
	}
	if !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 || before.Size() > 2*1024*1024 {
		return intent, apierr.Usagef("el intent debe ser un fichero regular privado (0600), sin enlace simbólico y de hasta 2 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return intent, apierr.ReadFileUsagef(path, err)
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Mode().Perm()&0077 != 0 {
		return intent, apierr.Usagef("el fichero de intent cambió durante su apertura")
	}
	body, err := io.ReadAll(io.LimitReader(f, 2*1024*1024+1))
	if err != nil {
		return intent, apierr.ReadFileUsagef(path, err)
	}
	if len(body) > 2*1024*1024 {
		return intent, apierr.Usagef("el intent supera 2 MiB")
	}
	o, err := crmDocumentJSON(body)
	if err != nil {
		return intent, err
	}
	if _, err := crmDocumentRecord(o, []string{"schema", "group", "action", "method", "path", "args", "body", "idempotency_key", "base_url", "profile", "environment", "credential_fingerprint", "api_version", "active_profile", "seal"}, nil); err != nil {
		return intent, err
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&intent); err != nil {
		return intent, apierr.Usagef("formato del intent incompatible")
	}
	return intent, nil
}

func (intent crmDocumentIntent) validate(group string, cc *cliContext) (crmDocumentOperation, map[string]any, error) {
	var op crmDocumentOperation
	for _, candidate := range crmDocumentOperations {
		if candidate.group == intent.Group && candidate.action == intent.Action && candidate.mutating() {
			op = candidate
			break
		}
	}
	if intent.Schema != 1 || intent.Group != group || op.action == "" || intent.Method != op.method {
		return op, nil, apierr.Usagef("el intent no pertenece a una mutación de este grupo")
	}
	if err := op.validateArgs(intent.Args); err != nil {
		return op, nil, err
	}
	if intent.Path != op.pathFor(intent.Args) {
		return op, nil, apierr.Usagef("el intent no conserva la ruta original")
	}
	if err := crmDocumentKey(intent.Key); err != nil {
		return op, nil, err
	}
	if err := validateCRMDocumentHeaders(intent.APIVersion, intent.ActiveProfile); err != nil {
		return op, nil, err
	}
	if intent.BaseURL != crmDocumentBaseURL() || intent.Profile != cc.res.Profile || intent.Environment != cc.res.Environment || intent.CredentialFingerprint != crmDocumentAuthFingerprint(cc.res.APIKey) {
		return op, nil, apierr.Permf("recovery requiere la credencial, servidor, entorno y perfil originales; no se despachó ninguna mutación")
	}
	want, err := hex.DecodeString(intent.signature(cc.res.APIKey))
	got, decodeErr := hex.DecodeString(intent.Seal)
	if err != nil || decodeErr != nil || !hmac.Equal(got, want) {
		return op, nil, apierr.Usagef("el sello del intent original no coincide; no se despachó ninguna mutación")
	}
	request, err := validateCRMDocumentBody(op, []byte(intent.Body))
	return op, request, err
}

func validateCRMDocumentHeaders(version, profile string) error {
	if version != "" && !crmDocumentDate(version) {
		return apierr.Usagef("--api-version debe usar YYYY-MM-DD")
	}
	if profile != "" && !crmDocumentID(profile, false) {
		return apierr.Usagef("--active-profile debe conservar el UUIDv7 del perfil gestionado")
	}
	return nil
}

func (intent crmDocumentIntent) headers() map[string]string {
	headers := crmDocumentHeaders(intent.APIVersion, intent.ActiveProfile)
	headers["Idempotency-Key"] = intent.Key
	return headers
}

func crmDocumentHeaders(version, profile string) map[string]string {
	headers := map[string]string{}
	if version != "" {
		headers["Factuarea-Version"] = version
	}
	if profile != "" {
		headers["X-Active-Profile"] = profile
	}
	return headers
}

func crmDocumentRecoveryHint(group, path, profile string, live bool) string {
	// The hint is a POSIX shell command. Single-quote literal paths/profile
	// names so a copied recovery command cannot expand $() or backticks.
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	guard := ""
	if live {
		guard = " --live"
	}
	return fmt.Sprintf("factuarea --profile %s%s crm %s recover --intent-file %s", quote(profile), guard, group, quote(path))
}
