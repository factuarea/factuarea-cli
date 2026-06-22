package buildinfo

// Inyectables por -ldflags en release. Defaults para `go run`/dev.
var (
	Version  = "dev"
	Commit   = "none"
	SpecHash = "unknown" // SpecHash: ya no se usa; el hash real se deriva del spec embebido vía spec.Hash()
)
