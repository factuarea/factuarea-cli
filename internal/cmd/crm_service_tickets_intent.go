package cmd

import (
	"crypto/hmac"
	"encoding/hex"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

// Reuse the frozen private checkpoint format and seal, with a closed ticket
// owner/operation binding. Document commands never accept this owner group.
type crmServiceTicketIntent crmDocumentIntent

func newCRMServiceTicketIntent(op crmServiceTicketOperation, args []string, body []byte, key, version, profile string, cc *cliContext) crmServiceTicketIntent {
	wire := crmDocumentIntent{Schema: 1, Group: "service-tickets", Action: op.action, Method: op.method, Path: op.pathFor(args),
		Args: append([]string{}, args...), Body: string(body), Key: key, BaseURL: crmDocumentBaseURL(), Profile: cc.res.Profile,
		Environment: cc.res.Environment, CredentialFingerprint: crmDocumentAuthFingerprint(cc.res.APIKey), APIVersion: version, ActiveProfile: profile}
	wire.Seal = wire.signature(cc.res.APIKey)
	return crmServiceTicketIntent(wire)
}

func (intent crmServiceTicketIntent) save(path string) error {
	return saveCRMDocumentIntent(path, crmDocumentIntent(intent))
}

func loadCRMServiceTicketIntent(path string) (crmServiceTicketIntent, error) {
	wire, err := loadCRMDocumentIntent(path)
	return crmServiceTicketIntent(wire), err
}

func (intent crmServiceTicketIntent) validate(cc *cliContext) (crmServiceTicketOperation, map[string]any, error) {
	var op crmServiceTicketOperation
	for _, candidate := range crmServiceTicketOperations {
		if candidate.action == intent.Action && candidate.mutating() {
			op = candidate
			break
		}
	}
	if intent.Schema != 1 || intent.Group != "service-tickets" || op.action == "" || intent.Method != op.method {
		return op, nil, apierr.Usagef("el intent no pertenece a una mutación nativa de tickets")
	}
	if err := op.validateArgs(intent.Args); err != nil {
		return op, nil, err
	}
	if intent.Path != op.pathFor(intent.Args) {
		return op, nil, apierr.Usagef("el intent no conserva la ruta original del ticket")
	}
	if err := crmDocumentKey(intent.Key); err != nil {
		return op, nil, err
	}
	if err := validateCRMDocumentHeaders(intent.APIVersion, intent.ActiveProfile); err != nil {
		return op, nil, err
	}
	if intent.BaseURL != crmDocumentBaseURL() || intent.Profile != cc.res.Profile || intent.Environment != cc.res.Environment || intent.CredentialFingerprint != crmDocumentAuthFingerprint(cc.res.APIKey) {
		return op, nil, apierr.Permf("recovery de ticket exige credencial, servidor, entorno y perfil originales; no se despachó la mutación")
	}
	wire := crmDocumentIntent(intent)
	want, _ := hex.DecodeString(wire.signature(cc.res.APIKey))
	got, err := hex.DecodeString(intent.Seal)
	if err != nil || !hmac.Equal(got, want) {
		return op, nil, apierr.Usagef("el sello del intent original del ticket no coincide")
	}
	request, err := validateCRMServiceTicketBody(op.action, []byte(intent.Body))
	return op, request, err
}
