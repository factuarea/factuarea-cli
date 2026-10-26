package spec

type Operation struct {
	OperationID    string
	Method         string
	Path           string
	Groups         []string
	Action         string
	Summary        string
	Deprecated     bool
	PathParams     []Param
	QueryParams    []Param
	Body           *Body
	BinaryResponse *BinaryResponse
	RequiredScope  string // x-required-scope ("" si ausente)
	Irreversible   bool   // x-irreversible (false si ausente)
	// IdempotencyRequired: cabecera `Idempotency-Key` requerida por el spec
	// (parámetro `in: header`, `name: Idempotency-Key`, `required: true`).
	IdempotencyRequired bool
	// HeaderParams: parámetros `in: header` declarados que la CLI no gestiona
	// por su cuenta (se excluyen `Idempotency-Key`, `Factuarea-Version`,
	// `X-Active-Profile` y las cabeceras de transporte). Si la operación declara
	// una respuesta 304 y no declara `If-None-Match`, se añade sintético: el 304
	// solo se alcanza con ese validador.
	HeaderParams []Param
	// ReturnsETag: alguna respuesta 2xx o 304 declara la cabecera `ETag`.
	ReturnsETag bool
	// NoRetry: la operación no debe reintentarse automáticamente (la CLI no
	// repite la llamada ante 429/5xx ni errores de transporte). Sale de la
	// extensión `x-no-retry: true`, de `x-speakeasy-retries.strategy: none` o de
	// `noRetryOperations`.
	NoRetry bool
}

type Param struct {
	Name        string
	In          string
	Required    bool
	Type        string
	Description string
}

type Body struct {
	Kind            string
	Example         string
	FileFields      []string
	FileArrayFields []string
	Fields          []BodyField
}

type BodyField struct {
	Name     string
	Type     string
	Required bool
	Enum     []string
	Nullable bool
	Kind     string
	Children []BodyField
}

// BinaryResponse describe una descarga cuyo 200 no es JSON. Dual marca la que
// también declara `application/json` (p. ej. el activo de una plantilla: imagen
// al leerlo, JSON si se pide el descriptor): el formato real lo decide el
// Content-Type de la respuesta, no el spec.
type BinaryResponse struct {
	ContentType string
	Dual        bool
}

func (o Operation) Mutating() bool {
	switch o.Method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

func (o Operation) Paginated() bool {
	for _, p := range o.QueryParams {
		if p.Name == "starting_after" {
			return true
		}
	}
	return false
}
