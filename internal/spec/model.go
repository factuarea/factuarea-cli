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

type BinaryResponse struct{ ContentType string }

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
