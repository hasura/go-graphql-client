package graphql

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

var (
	// regular expression for the graphql variable name
	// reference: https://spec.graphql.org/June2018/#sec-Names
	regexVariableName = regexp.MustCompile(`\$([_A-Za-z][_0-9A-Za-z]*)`)

	errBuildQueryRequired = errors.New("no graphql query to be built")
)

type queryBuilderItem struct {
	query        string
	binding      interface{}
	requiredVars []string
}

// Builder is used to efficiently build dynamic queries and variables
// It helps construct multiple queries to a single request that needs to be conditionally added
type Builder struct {
	context   context.Context
	queries   []queryBuilderItem
	variables map[string]interface{}
}

// QueryBinding the type alias of interface tuple
// that includes the query string without fields and the binding type
type QueryBinding [2]interface{}

// NewBuilder creates an empty Builder instance
func NewBuilder() Builder {
	return Builder{
		variables: make(map[string]interface{}),
	}
}

// Bind returns the new Builder with the inputted query
func (b Builder) Context(ctx context.Context) Builder {
	return Builder{
		context:   ctx,
		queries:   b.queries,
		variables: b.variables,
	}
}

// Bind returns the new Builder with a new query and target data binding
func (b Builder) Bind(query string, binding interface{}) Builder {
	return Builder{
		context: b.context,
		queries: append(b.queries, queryBuilderItem{
			query,
			binding,
			findAllVariableNames(query),
		}),
		variables: b.variables,
	}
}

// Variables returns the new Builder with the inputted variables
func (b Builder) Variable(key string, value interface{}) Builder {
	return Builder{
		context:   b.context,
		queries:   b.queries,
		variables: setMapValue(b.variables, key, value),
	}
}

// Variables returns the new Builder with the inputted variables
func (b Builder) Variables(variables map[string]interface{}) Builder {
	return Builder{
		context:   b.context,
		queries:   b.queries,
		variables: mergeMap(b.variables, variables),
	}
}

// Unbind returns the new Builder with query items and related variables removed
func (b Builder) Unbind(query string, extra ...string) Builder {
	var newQueries []queryBuilderItem
	newVars := make(map[string]interface{})

	for _, q := range b.queries {
		if q.query == query || sliceStringContains(extra, q.query) {
			continue
		}
		newQueries = append(newQueries, q)
		if len(b.variables) > 0 {
			for _, k := range q.requiredVars {
				if v, ok := b.variables[k]; ok {
					newVars[k] = v
				}
			}
		}
	}

	return Builder{
		context:   b.context,
		queries:   newQueries,
		variables: newVars,
	}
}

// RemoveQuery returns the new Builder with query items removed
// this method only remove query items only,
// to remove both query and variables, use Remove instead
func (b Builder) RemoveQuery(query string, extra ...string) Builder {
	var newQueries []queryBuilderItem

	for _, q := range b.queries {
		if q.query != query && !sliceStringContains(extra, q.query) {
			newQueries = append(newQueries, q)
		}
	}

	return Builder{
		context:   b.context,
		queries:   newQueries,
		variables: b.variables,
	}
}

// RemoveQuery returns the new Builder with variable fields removed
func (b Builder) RemoveVariable(key string, extra ...string) Builder {
	newVars := make(map[string]interface{})
	for k, v := range b.variables {
		if k != key && !sliceStringContains(extra, k) {
			newVars[k] = v
		}
	}

	return Builder{
		context:   b.context,
		queries:   b.queries,
		variables: newVars,
	}
}

// Build query and variable interfaces
func (b Builder) Build() ([]QueryBinding, map[string]interface{}, error) {
	if len(b.queries) == 0 {
		return nil, nil, errBuildQueryRequired
	}

	var requiredVars []string
	for _, q := range b.queries {
		requiredVars = append(requiredVars, q.requiredVars...)
	}
	variableLength := len(b.variables)
	requiredVariableLength := len(requiredVars)
	isMismatchedVariables := variableLength != requiredVariableLength
	if !isMismatchedVariables && requiredVariableLength > 0 {
		for _, varName := range requiredVars {
			if _, ok := b.variables[varName]; !ok {
				isMismatchedVariables = true
				break
			}
		}
	}
	if isMismatchedVariables {
		varNames := make([]string, 0, variableLength)
		for k := range b.variables {
			varNames = append(varNames, k)
		}
		return nil, nil, fmt.Errorf("mismatched variables; want: %+v; got: %+v", requiredVars, varNames)
	}

	query := make([]QueryBinding, 0, len(b.queries))
	for _, q := range b.queries {
		query = append(query, [2]interface{}{q.query, q.binding})
	}
	return query, b.variables, nil
}

// Query builds parameters and executes the GraphQL query request
func (b Builder) Query(c *Client, options ...Option) error {
	q, v, err := b.Build()
	if err != nil {
		return err
	}
	ctx := b.context
	if ctx == nil {
		ctx = context.TODO()
	}
	return c.Query(ctx, &q, v, options...)
}

// Mutate builds parameters and executes the GraphQL query request
func (b Builder) Mutate(c *Client, options ...Option) error {
	q, v, err := b.Build()
	if err != nil {
		return err
	}
	ctx := b.context
	if ctx == nil {
		ctx = context.TODO()
	}
	return c.Mutate(ctx, &q, v, options...)
}

func setMapValue(src map[string]interface{}, key string, value interface{}) map[string]interface{} {
	if src == nil {
		src = make(map[string]interface{})
	}
	src[key] = value
	return src
}

func mergeMap(src map[string]interface{}, dest map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range src {
		setMapValue(result, k, v)
	}
	for k, v := range dest {
		setMapValue(result, k, v)
	}
	return result
}

func sliceStringContains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

func findAllVariableNames(query string) []string {
	var results []string
	for _, names := range regexVariableName.FindAllStringSubmatch(query, -1) {
		results = append(results, names[1])
	}
	return results
}
