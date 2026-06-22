package spec

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

//go:embed openapi.json
var Raw []byte

// Hash devuelve el sha256 hex del spec embebido (se expone en `factuarea version`).
func Hash() string {
	sum := sha256.Sum256(Raw)
	return hex.EncodeToString(sum[:])
}
