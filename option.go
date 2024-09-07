package graphql

// OptionType represents the logic of graphql query construction
type OptionType string

const (
	OptionTypeOperationDirective OptionType = "operation_directive"
)

// Option abstracts an extra render interface for the query string
// They are optional parts. By default GraphQL queries can request data without them
type Option interface {
	// Type returns the supported type of the renderer
	// available types: operation_name and operation_directive
	Type() OptionType
}

// operationNameOption represents the operation name render component
type operationNameOption struct {
	name string
}

func (ono operationNameOption) Type() OptionType {
	return "operation_name"
}

func (ono operationNameOption) String() string {
	return ono.name
}

// OperationName creates the operation name option
func OperationName(name string) Option {
	return operationNameOption{name}
}

// bind the struct pointer to decode extensions from response
type bindExtensionsOption struct {
	value any
}

func (ono bindExtensionsOption) Type() OptionType {
	return "bind_extensions"
}

// BindExtensions bind the struct pointer to decode extensions from json response
func BindExtensions(value any) Option {
	return bindExtensionsOption{value: value}
}
