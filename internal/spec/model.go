package spec

// Operation es la representación que el CLI consume de una operación del spec:
// el operationId resuelto (grupos + acción), sus parámetros, su body y, si la
// respuesta es binaria, su content-type. El generador (Task 4) emite la tabla de
// comandos a partir de este modelo.
type Operation struct {
	OperationID    string
	Method         string // GET, POST, PUT, PATCH, DELETE
	Path           string // /invoices/{invoice}
	Groups         []string
	Action         string
	Summary        string
	Deprecated     bool
	PathParams     []Param
	QueryParams    []Param
	Body           *Body
	BinaryResponse *BinaryResponse
}

// Param es un parámetro de path o query.
type Param struct {
	Name        string
	In          string // path | query
	Required    bool
	Type        string
	Description string
}

// Body describe el cuerpo de la petición: json (con un example serializado) o
// multipart (con los nombres de los campos binarios a subir como fichero).
type Body struct {
	Kind       string   // "json" | "multipart"
	Example    string   // JSON example (solo json)
	FileFields []string // campos binarios (solo multipart)
}

// BinaryResponse marca una operación cuya respuesta 200 es un binario (PDF, etc.).
type BinaryResponse struct{ ContentType string }

// Mutating indica si la operación modifica estado (todo lo que no es GET).
func (o Operation) Mutating() bool {
	switch o.Method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

// Paginated indica si la operación pagina por cursor (query param starting_after).
func (o Operation) Paginated() bool {
	for _, p := range o.QueryParams {
		if p.Name == "starting_after" {
			return true
		}
	}
	return false
}
