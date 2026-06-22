package spec

import "strings"

const operationIDPrefix = "public-api.v1."

// Resolve trocea el operationId en (grupos, acción) espejando resolveNamespace
// del backend: quita el prefijo, separa por '.', el último segmento es la acción
// y el resto son los grupos. Todos en kebab-case. ok=false si no cumple el
// invariante (prefijo + >=2 segmentos).
func Resolve(operationID string) (groups []string, action string, ok bool) {
	if !strings.HasPrefix(operationID, operationIDPrefix) {
		return nil, "", false
	}
	rest := strings.TrimPrefix(operationID, operationIDPrefix)
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

// ToKebab pasa un segmento de operationId a kebab-case para el CLI.
func ToKebab(s string) string { return strings.ReplaceAll(s, "_", "-") }
