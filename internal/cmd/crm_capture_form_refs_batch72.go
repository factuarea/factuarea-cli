package cmd

import (
	"encoding/json"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/spf13/cobra"
)

const (
	serviceFormReferencesPath      = "/v1/crm/capture/service-form-references"
	serviceFormReferencesScope     = "crm_capture_forms:read"
	serviceFormReferencesOperation = "crm_capture_forms.references"
	serviceFormReferencesID        = "crmGetServiceFormReferences"
	serviceFormReferencesCommand   = "factuarea crm capture-forms service-form-references"
	serviceFormReferencesSummary   = "Consulta colas, políticas, calendarios y responsables actuales para un formulario de servicio"
)

// V1 exposes the native UUID as id. These types retain that alias and the
// distinct historical User UUID contract; no installation key is returned.
type serviceFormReferenceUUID string
type serviceFormResponsibleUUID string

type serviceFormReferenceOption struct {
	ID   serviceFormReferenceUUID `json:"id"`
	Name string                   `json:"name"`
}

type serviceFormReferenceCalendar struct {
	ID      serviceFormReferenceUUID `json:"id"`
	Name    string                   `json:"name"`
	Version int64                    `json:"version"`
}

type serviceFormReferenceResponsible struct {
	ID   serviceFormResponsibleUUID `json:"id"`
	Name string                     `json:"name"`
}

type serviceFormReferencesEnvelope struct {
	Data struct {
		Queues               []serviceFormReferenceOption      `json:"queues"`
		Policies             []serviceFormReferenceOption      `json:"policies"`
		Calendars            []serviceFormReferenceCalendar    `json:"calendars"`
		ResponsibleUsers     []serviceFormReferenceResponsible `json:"responsible_users"`
		AuthorizationVersion string                            `json:"authorization_version"`
	} `json:"data"`
}

func serviceFormReferencesSchema() nativeSchema {
	id := nUUID()
	id["format"] = "uuid"
	userID := nHost()
	userID["format"] = "uuid"
	option := nObject(nativeSchema{"id": id, "name": nText(1, 150)}, "id", "name")
	calendar := nObject(nativeSchema{"id": id, "name": nText(1, 150), "version": nativeSchema{"type": "integer", "minimum": 1}}, "id", "name", "version")
	responsible := nObject(nativeSchema{"id": userID, "name": nText(1, 150)}, "id", "name")
	options := func(item nativeSchema) nativeSchema {
		list := nArray(item)
		list["maxItems"] = 100
		return list
	}
	return nEnvelope(nObject(nativeSchema{
		"queues":                options(option),
		"policies":              options(option),
		"calendars":             options(calendar),
		"responsible_users":     options(responsible),
		"authorization_version": nativeSchema{"type": "string", "minLength": 1},
	}, "queues", "policies", "calendars", "responsible_users", "authorization_version"))
}

func serviceFormReferencesManifest() []manifestEntry {
	return []manifestEntry{{
		Command: serviceFormReferencesCommand, OperationID: serviceFormReferencesID,
		CrmOperation: serviceFormReferencesOperation, Summary: serviceFormReferencesSummary,
		Args: []string{}, Flags: []flagInfo{}, Method: "GET", NativePath: serviceFormReferencesPath,
		AuthRealm: string(client.NativeStaff), OperationIDSource: "native_v1_surface",
		RequiredScope: serviceFormReferencesScope, RequiredScopes: []string{serviceFormReferencesScope},
		QuerySchemas:    map[string]json.RawMessage{},
		ResponseSchemas: map[string]json.RawMessage{"200": nRaw(serviceFormReferencesSchema())},
	}}
}

func registerServiceFormReferencesCommand(root *cobra.Command) {
	var crm *cobra.Command
	for _, child := range root.Commands() {
		if child.Name() == "crm" {
			crm = child
			break
		}
	}
	if crm == nil {
		panic("native CRM registration required")
	}
	group := &cobra.Command{Use: "capture-forms", Short: "Formularios de captación CRM", Args: groupArgs}
	group.AddCommand(newServiceFormReferencesCommand())
	crm.AddCommand(group)
}

func newServiceFormReferencesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "service-form-references", Short: serviceFormReferencesSummary,
		Long: serviceFormReferencesSummary + ".\nLa credencial existente fija la empresa y el entorno; el servidor revalida los permisos actuales. Las opciones V1 conservan id/name y los calendarios también su version. La política y el calendario se eligen por separado. Esta lectura GET no acepta parámetros ni cuerpo.",
		Args: UsageArgs(cobra.NoArgs),
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cc, err := newCLIContext(globalsFrom(cmd), "")
		if err != nil {
			return err
		}
		scopes, err := cc.scopes(cmd.Context())
		if err != nil {
			return err
		}
		if !safety.HasScope(scopes, serviceFormReferencesScope) {
			return apierr.Permf("falta scope nativo %s", serviceFormReferencesScope)
		}
		response, err := cc.client.DoNative(cmd.Context(), client.NativeRequest{
			Realm: client.NativeStaff, Method: "GET", Path: serviceFormReferencesPath,
		})
		if err != nil {
			return reportNativeOwnerError(cmd, cc, nativeOwnerOp{Realm: client.NativeStaff}, err)
		}
		if err := validateServiceFormReferencesResponse(response); err != nil {
			return reportNativeOwnerError(cmd, cc, nativeOwnerOp{Realm: client.NativeStaff}, err)
		}
		return output.PrintBody(cmd.OutOrStdout(), response.Body, cc.format)
	}
	return cmd
}

func validateServiceFormReferencesResponse(response *client.Response) error {
	invalid := func() error {
		return &apierr.APIError{StatusCode: 502, Type: "api_error", Code: "invalid_native_response", Message: "La respuesta de referencias de formularios no coincide con el contrato V1 nativo."}
	}
	if response == nil || response.StatusCode != 200 {
		return invalid()
	}
	value, err := nativeOriginalJSON(response.Body)
	if err != nil || nativeOwnerSchema(serviceFormReferencesSchema(), value) != nil {
		return invalid()
	}
	var wire serviceFormReferencesEnvelope
	if decodeKnowledgeJSON(response.Body, &wire) != nil || serviceFormReferencesBlank(wire.Data.AuthorizationVersion) {
		return invalid()
	}
	// Each owner collection has its own identities. Policy and calendar choices
	// remain independent, including when one of those collections is empty.
	for _, options := range [][]serviceFormReferenceOption{wire.Data.Queues, wire.Data.Policies} {
		seen := map[serviceFormReferenceUUID]bool{}
		for _, option := range options {
			if seen[option.ID] || serviceFormReferencesBlank(option.Name) {
				return invalid()
			}
			seen[option.ID] = true
		}
	}
	calendars := map[serviceFormReferenceUUID]bool{}
	for _, option := range wire.Data.Calendars {
		if calendars[option.ID] || serviceFormReferencesBlank(option.Name) {
			return invalid()
		}
		calendars[option.ID] = true
	}
	users := map[serviceFormResponsibleUUID]bool{}
	for _, option := range wire.Data.ResponsibleUsers {
		if users[option.ID] || serviceFormReferencesBlank(option.Name) {
			return invalid()
		}
		users[option.ID] = true
	}
	return nil
}

func serviceFormReferencesBlank(value string) bool {
	// Match the producer's PHP trim() boundary without normalizing wire bytes.
	return strings.Trim(value, " \t\n\r\x00\x0b") == ""
}
