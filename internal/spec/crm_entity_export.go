package spec

import (
	_ "embed"
	"encoding/json"
)

// This feature-local descriptor preserves the published owner OpenAPI. It does
// not regenerate legacy exports or confer a current scope, record or field grant.
//
//go:embed crm_entity_export_contract.json
var crmEntityExportDescriptor []byte

type CrmEntityExportOperation struct {
	Method        string `json:"method"`
	Path          string `json:"path"`
	OperationID   string `json:"operation_id"`
	RequiredScope string `json:"required_scope"`
}

type CrmEntityExportDataset struct {
	Columns []string          `json:"columns"`
	Filters map[string]string `json:"filters"`
	Period  json.RawMessage   `json:"period"`
}

type CrmEntityExportContract struct {
	Types             []string                            `json:"types"`
	Operations        map[string]CrmEntityExportOperation `json:"operations"`
	Datasets          map[string]CrmEntityExportDataset   `json:"dataset_matrix"`
	RequestKeys       []string                            `json:"request_keys"`
	Statuses          []string                            `json:"statuses"`
	RequestMaxBytes   int                                 `json:"request_max_bytes"`
	SelectionMaxBytes int                                 `json:"selection_max_bytes"`
	ColumnsMin        int                                 `json:"columns_min"`
	ColumnsMax        int                                 `json:"columns_max"`
}

func NativeCrmEntityExportContract() CrmEntityExportContract {
	var contract CrmEntityExportContract
	if err := json.Unmarshal(crmEntityExportDescriptor, &contract); err != nil {
		panic("invalid embedded native CRM entity export descriptor")
	}
	return contract
}
