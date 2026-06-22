package buildinfo

// Inyectables por -ldflags en release. Defaults para `go run`/dev.
var (
	Version  = "dev"
	Commit   = "none"
	SpecHash = "unknown" // hash del openapi.json embebido (lo fija el Plan 2)
)
