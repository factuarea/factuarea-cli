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
	RequiredScope  string   // x-required-scope ("" si ausente)
	RequiredScopes []string // Native all-of scopes; never a local grant.
	NativeContract *NativeContract
	Irreversible   bool   // x-irreversible (false si ausente)
	CrmOperation   string // Original native x-crm-operation, never a grant.
	Pagination     *Pagination
	// IdempotencyRequired: cabecera `Idempotency-Key` requerida por el spec
	// (parámetro `in: header`, `name: Idempotency-Key`, `required: true`).
	IdempotencyRequired bool
}

type NativeContract struct {
	MaxBodyBytes    int
	RequestSchema   string
	ResponseSchemas map[string]string
	QuerySchemas    map[string]string
}

type Param struct {
	Name        string
	In          string
	Required    bool
	Type        string
	Description string
	Format      string
	Pattern     string
}

type Pagination struct {
	Kind, QueryParameter, ItemsPath, MorePath, CursorPath string
	CurrentPagePath, LastPagePath                         string
	IdentityPath                                          string
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
	if o.Pagination != nil {
		return true
	}
	for _, p := range o.QueryParams {
		if p.Name == "starting_after" {
			return true
		}
	}
	return false
}
