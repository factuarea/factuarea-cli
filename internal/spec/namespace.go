package spec

import "strings"

const operationIDPrefix = "public-api.v1."

func Resolve(operationID string) (groups []string, action string, ok bool) {
	// The native owner retains these exact operation IDs in its OpenAPI.
	// Reuse the existing OAuth/original command paths, without renaming the
	// operation ID or inferring a namespace from CamelCase.
	native := map[string][]string{
		"crmGetOAuthCredentialContext":    {"crm", "credential-context"},
		"crmGetLeadCreateOriginal":        {"crm", "originals", "lead-create"},
		"crmGetOpportunityCreateOriginal": {"crm", "originals", "opportunity-create"},
		"crmGetOpportunityStageOriginal":  {"crm", "originals", "opportunity-stage"},
		"crmGetCrmActivityCreateOriginal": {"crm", "originals", "activity-create"},
	}
	if command, exists := native[operationID]; exists {
		return append([]string(nil), command[:len(command)-1]...), command[len(command)-1], true
	}
	if routeName, exists := nativeCRMRouteNames[operationID]; exists {
		prefix := operationIDPrefix
		if strings.HasPrefix(routeName, "v1.") {
			prefix = "v1."
		}
		return resolveCommandRoute(routeName, prefix)
	}
	return resolveCommandRoute(operationID, operationIDPrefix)
}

func resolveCommandRoute(operationID, prefix string) (groups []string, action string, ok bool) {
	if !strings.HasPrefix(operationID, prefix) {
		return nil, "", false
	}
	rest := strings.TrimPrefix(operationID, prefix)
	segs := strings.Split(rest, ".")
	if len(segs) < 2 {
		return nil, "", false
	}
	action = ToKebab(segs[len(segs)-1])
	for _, s := range segs[:len(segs)-1] {
		groups = append(groups, ToKebab(s))
	}
	return groups, action, true
}

func ToKebab(s string) string { return strings.ReplaceAll(s, "_", "-") }
