package spec

import "strings"

// OperationIDPrefix es el prefijo canónico que el backend da a toda ruta de la
// API pública v1 (`->name('public-api.v1.…')`). El árbol de comandos del CLI se
// deriva EXCLUSIVAMENTE de este identificador: ni el path ni el tag del spec
// intervienen. Por eso el eje de empresa del contrato v1
// (`/companies/{company}/<recurso>`) NO reagrupa ningún comando: el segmento de
// empresa es un parámetro de ruta más y el nombre de la ruta sigue mandando.
//
// Está exportado porque el generador (`internal/gen`) lo cita en el mensaje del
// fallo duro que emite cuando el contrato trae un identificador no canónico:
// una sola fuente para la cadena, en vez de dos literales que se desincronizan.
const OperationIDPrefix = "public-api.v1."

func Resolve(operationID string) (groups []string, action string, ok bool) {
	if !strings.HasPrefix(operationID, OperationIDPrefix) {
		return nil, "", false
	}
	rest := strings.TrimPrefix(operationID, OperationIDPrefix)
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
