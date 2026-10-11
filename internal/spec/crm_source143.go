package spec

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
)

//go:embed crm_source143.openapi.json
var crmSource143Raw []byte

const CrmSource143NativeHash = "59b6cd2664aa8524e96a7600565b70b5eae5f6beaa7527913d703adf653a2fca"

// Reuses the schema boundary, never a manual snapshot or the public spec.
func NativeCrmSource143Contract() (*CrmSource139Contract, error) {
	var contract CrmSource139Contract
	decoder := json.NewDecoder(bytes.NewReader(crmSource143Raw))
	decoder.UseNumber()
	if decoder.Decode(&contract) != nil || contract.NativeHash != CrmSource143NativeHash || contract.Profile.State != "unreleased" || contract.Profile.Published || contract.Profile.Ratified || contract.Profile.DeliveryEnabled || len(contract.Capabilities) != 0 {
		return nil, errors.New("contrato Source143 sin gate privado nativo")
	}
	for key, op := range contract.Operations {
		if (op.Method != "GET" && op.Method != "POST") || op.ID() == "" || op.Scope() == "" {
			return nil, errors.New("operación Source143 sin carrier nativo: " + key)
		}
	}
	if len(contract.Operations) != 5 {
		return nil, errors.New("selección Source143 incompleta")
	}
	return &contract, nil
}
