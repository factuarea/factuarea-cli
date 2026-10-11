// Generated from the delivered private native artifact; do not substitute schemas or publication gates.
package spec

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
)

//go:embed crm_native134.openapi.json
var crmNative134Raw []byte

const CrmNative134ArtifactHash = "3667e01b82dea255c23b2eecb80e6e600baa445b5b7a2beb5a65eb978a1fc073"
const CrmNative134SelectedHash = "9239ea75e8b592ed92484b0b455093d0671daaec758512cd8183e9c4f605f787"
const CrmNative134ManifestHash = "653247d4492095fb40e06c36e20a61ec4e0ba32165d0f393378eeffe9872d901"

// Reuses the same native schema validator and does not widen the published contract.
func NativeCrm134Contract() (*CrmSource139Contract, error) {
	digest := sha256.Sum256(crmNative134Raw)
	if hex.EncodeToString(digest[:]) != CrmNative134SelectedHash {
		return nil, errors.New("selección Native134 distinta del export nativo fijado")
	}
	var envelope struct {
		CrmSource139Contract
		ManifestHash string                           `json:"source_manifest_sha256"`
		Boundaries   map[string]CrmSource139Operation `json:"boundaries"`
	}
	decoder := json.NewDecoder(bytes.NewReader(crmNative134Raw))
	decoder.UseNumber()
	if decoder.Decode(&envelope) != nil || envelope.NativeHash != CrmNative134ArtifactHash || envelope.ManifestHash != CrmNative134ManifestHash || envelope.Profile.State != "unreleased" || envelope.Profile.Published || envelope.Profile.Ratified || envelope.Profile.DeliveryEnabled || len(envelope.Capabilities) != 0 || len(envelope.Operations) != 7 || len(envelope.Boundaries) != 1 {
		return nil, errors.New("contrato Native134 sin su gate privado nativo")
	}
	for _, operation := range envelope.Operations {
		if operation.ID() == "" || operation.Scope() == "" || (operation.Method != "GET" && operation.Method != "POST" && operation.Method != "PATCH") {
			return nil, errors.New("operación Native134 sin carrier o scope nativo")
		}
	}
	boundary, exists := envelope.Boundaries["restore_intent"]
	if !exists || boundary.Method != "POST" || boundary.Path != "/crm/documents/{document}/restore" || boundary.ID() != "crmRestoreCommercialDocument" || boundary.Scope() != "crm_documents:write" {
		return nil, errors.New("la recuperación Restore requiere el esquema de su POST original")
	}
	// A validation-only boundary: no command or dispatch is generated for it.
	envelope.Operations["restore_intent"] = boundary
	return &envelope.CrmSource139Contract, nil
}
