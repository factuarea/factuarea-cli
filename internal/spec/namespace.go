package spec

import "strings"

const operationIDPrefix = "public-api.v1."

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

func ToKebab(s string) string { return strings.ReplaceAll(s, "_", "-") }
