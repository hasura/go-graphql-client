package graphql

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hasura/go-graphql-client/pkg/jsonutil"
)

// Doer interface has the method required to use a type as custom http client.
// The net/*http.Client type satisfies this interface.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// This function allows you to tweak the HTTP request. It might be useful to set authentication
// headers  amongst other things
type RequestModifier func(*http.Request)

// Client is a GraphQL client.
type Client struct {
	url             string // GraphQL server URL.
	httpClient      Doer
	requestModifier RequestModifier
	debug           bool
}

// NewClient creates a GraphQL client targeting the specified GraphQL server URL.
// If httpClient is nil, then http.DefaultClient is used.
func NewClient(url string, httpClient Doer) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		url:             url,
		httpClient:      httpClient,
		requestModifier: nil,
	}
}

// Query executes a single GraphQL query request,
// with a query derived from q, populating the response into it.
// q should be a pointer to struct that corresponds to the GraphQL schema.
func (c *Client) Query(ctx context.Context, q any, variables map[string]any, options ...Option) error {
	return c.do(ctx, queryOperation, q, variables, options...)
}

// NamedQuery executes a single GraphQL query request, with operation name
//
// Deprecated: this is the shortcut of Query method, with NewOperationName option
func (c *Client) NamedQuery(ctx context.Context, name string, q any, variables map[string]any, options ...Option) error {
	return c.do(ctx, queryOperation, q, variables, append(options, OperationName(name))...)
}

// Mutate executes a single GraphQL mutation request,
// with a mutation derived from m, populating the response into it.
// m should be a pointer to struct that corresponds to the GraphQL schema.
func (c *Client) Mutate(ctx context.Context, m any, variables map[string]any, options ...Option) error {
	return c.do(ctx, mutationOperation, m, variables, options...)
}

// NamedMutate executes a single GraphQL mutation request, with operation name
//
// Deprecated: this is the shortcut of Mutate method, with NewOperationName option
func (c *Client) NamedMutate(ctx context.Context, name string, m any, variables map[string]any, options ...Option) error {
	return c.do(ctx, mutationOperation, m, variables, append(options, OperationName(name))...)
}

// Query executes a single GraphQL query request,
// with a query derived from q, populating the response into it.
// q should be a pointer to struct that corresponds to the GraphQL schema.
// return raw bytes message.
func (c *Client) QueryRaw(ctx context.Context, q any, variables map[string]any, options ...Option) ([]byte, error) {
	return c.doRaw(ctx, queryOperation, q, variables, options...)
}

// NamedQueryRaw executes a single GraphQL query request, with operation name
// return raw bytes message.
func (c *Client) NamedQueryRaw(ctx context.Context, name string, q any, variables map[string]any, options ...Option) ([]byte, error) {
	return c.doRaw(ctx, queryOperation, q, variables, append(options, OperationName(name))...)
}

// MutateRaw executes a single GraphQL mutation request,
// with a mutation derived from m, populating the response into it.
// m should be a pointer to struct that corresponds to the GraphQL schema.
// return raw bytes message.
func (c *Client) MutateRaw(ctx context.Context, m any, variables map[string]any, options ...Option) ([]byte, error) {
	return c.doRaw(ctx, mutationOperation, m, variables, options...)
}

// NamedMutateRaw executes a single GraphQL mutation request, with operation name
// return raw bytes message.
func (c *Client) NamedMutateRaw(ctx context.Context, name string, m any, variables map[string]any, options ...Option) ([]byte, error) {
	return c.doRaw(ctx, mutationOperation, m, variables, append(options, OperationName(name))...)
}

// buildQueryAndOptions the common method to build query and options
func (c *Client) buildQueryAndOptions(op operationType, v any, variables map[string]any, options ...Option) (string, *constructOptionsOutput, error) {
	var query string
	var err error
	var optionOutput *constructOptionsOutput
	switch op {
	case queryOperation:
		query, optionOutput, err = constructQuery(v, variables, options...)
	case mutationOperation:
		query, optionOutput, err = constructMutation(v, variables, options...)
	default:
		err = fmt.Errorf("invalid operation type: %v", op)
	}

	if err != nil {
		return "", nil, Errors{newError(ErrGraphQLEncode, err)}
	}
	return query, optionOutput, nil
}

// Request the common method that send graphql request
func (c *Client) request(ctx context.Context, query string, variables map[string]any, options *constructOptionsOutput) ([]byte, []byte, *http.Response, io.Reader, Errors) {
	in := GraphQLRequestPayload{
		Query:     query,
		Variables: variables,
	}

	if options != nil {
		in.OperationName = options.operationName
	}

	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(in)
	if err != nil {
		return nil, nil, nil, nil, Errors{newError(ErrGraphQLEncode, err)}
	}

	reqReader := bytes.NewReader(buf.Bytes())
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, reqReader)
	if err != nil {
		e := newError(ErrRequestError, fmt.Errorf("problem constructing request: %w", err))
		if c.debug {
			e = e.withRequest(request, reqReader)
		}
		return nil, nil, nil, nil, Errors{e}
	}
	request.Header.Add("Content-Type", "application/json")

	if c.requestModifier != nil {
		c.requestModifier(request)
	}

	resp, err := c.httpClient.Do(request)

	if c.debug {
		_, _ = reqReader.Seek(0, io.SeekStart)
	}

	if err != nil {
		e := newError(ErrRequestError, err)
		if c.debug {
			e = e.withRequest(request, reqReader)
		}
		return nil, nil, nil, nil, Errors{e}
	}
	defer resp.Body.Close()

	r := resp.Body

	if resp.Header.Get("Content-Encoding") == "gzip" {
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, nil, nil, nil, Errors{newError(ErrJsonDecode, fmt.Errorf("problem trying to create gzip reader: %w", err))}
		}
		defer gr.Close()
		r = gr
	}

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		err := newError(ErrRequestError, NetworkError{
			statusCode: resp.StatusCode,
			body:       string(b),
		})

		if c.debug {
			err = err.withRequest(request, reqReader)
		}
		return nil, nil, nil, nil, Errors{err}
	}

	var out struct {
		Data       *json.RawMessage
		Extensions *json.RawMessage
		Errors     Errors
	}

	// copy the response reader for debugging
	var respReader *bytes.Reader
	if c.debug {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, nil, nil, Errors{newError(ErrJsonDecode, err)}
		}
		respReader = bytes.NewReader(body)
		r = io.NopCloser(respReader)
	}

	err = json.NewDecoder(r).Decode(&out)

	if c.debug {
		_, _ = respReader.Seek(0, io.SeekStart)
	}

	if err != nil {
		we := newError(ErrJsonDecode, err)
		if c.debug {
			we = we.withRequest(request, reqReader).
				withResponse(resp, respReader)
		}
		return nil, nil, nil, nil, Errors{we}
	}

	var rawData []byte
	if out.Data != nil && len(*out.Data) > 0 {
		rawData = []byte(*out.Data)
	}

	var extensions []byte
	if out.Extensions != nil && len(*out.Extensions) > 0 {
		extensions = []byte(*out.Extensions)
	}

	if len(out.Errors) > 0 {
		if c.debug && (out.Errors[0].Extensions == nil || out.Errors[0].Extensions["request"] == nil) {
			out.Errors[0] = out.Errors[0].
				withRequest(request, reqReader).
				withResponse(resp, respReader)
		}

		return rawData, extensions, resp, respReader, out.Errors
	}

	return rawData, extensions, resp, respReader, nil
}

// do executes a single GraphQL operation.
// return raw message and error
func (c *Client) doRaw(ctx context.Context, op operationType, v any, variables map[string]any, options ...Option) ([]byte, error) {
	query, optionsOutput, err := c.buildQueryAndOptions(op, v, variables, options...)
	if err != nil {
		return nil, err
	}
	data, _, _, _, errs := c.request(ctx, query, variables, optionsOutput)
	if len(errs) > 0 {
		return data, errs
	}

	return data, nil
}

// do executes a single GraphQL operation and unmarshal json.
func (c *Client) do(ctx context.Context, op operationType, v any, variables map[string]any, options ...Option) error {
	query, optionsOutput, err := c.buildQueryAndOptions(op, v, variables, options...)
	if err != nil {
		return err
	}
	data, extData, resp, respBuf, errs := c.request(ctx, query, variables, optionsOutput)

	return c.processResponse(v, data, optionsOutput.extensions, extData, resp, respBuf, errs)
}

// Executes a pre-built query and unmarshals the response into v. Unlike the Query method you have to specify in the query the
// fields that you want to receive as they are not inferred from v. This method is useful if you need to build the query dynamically.
func (c *Client) Exec(ctx context.Context, query string, v any, variables map[string]any, options ...Option) error {
	optionsOutput, err := constructOptions(options)
	if err != nil {
		return err
	}

	data, extData, resp, respBuf, errs := c.request(ctx, query, variables, optionsOutput)
	return c.processResponse(v, data, optionsOutput.extensions, extData, resp, respBuf, errs)
}

// Executes a pre-built query and returns the raw json message. Unlike the Query method you have to specify in the query the
// fields that you want to receive as they are not inferred from the interface. This method is useful if you need to build the query dynamically.
func (c *Client) ExecRaw(ctx context.Context, query string, variables map[string]any, options ...Option) ([]byte, error) {
	optionsOutput, err := constructOptions(options)
	if err != nil {
		return nil, err
	}

	data, _, _, _, errs := c.request(ctx, query, variables, optionsOutput)
	if len(errs) > 0 {
		return data, errs
	}
	return data, nil
}

// ExecRawWithExtensions execute a pre-built query and returns the raw json message and a map with extensions (values also as raw json objects). Unlike the
// Query method you have to specify in the query the fields that you want to receive as they are not inferred from the interface. This method
// is useful if you need to build the query dynamically.
func (c *Client) ExecRawWithExtensions(ctx context.Context, query string, variables map[string]any, options ...Option) ([]byte, []byte, error) {
	optionsOutput, err := constructOptions(options)
	if err != nil {
		return nil, nil, err
	}

	data, ext, _, _, errs := c.request(ctx, query, variables, optionsOutput)
	if len(errs) > 0 {
		return data, ext, errs
	}
	return data, ext, nil
}

func (c *Client) processResponse(v any, data []byte, extensions any, rawExtensions []byte, resp *http.Response, respBuf io.Reader, errs Errors) error {
	if len(data) > 0 {
		err := jsonutil.UnmarshalGraphQL(data, v)
		if err != nil {
			we := newError(ErrGraphQLDecode, err)
			if c.debug {
				we = we.withResponse(resp, respBuf)
			}
			errs = append(errs, we)
		}
	}

	if len(rawExtensions) > 0 && extensions != nil {
		err := json.Unmarshal(rawExtensions, extensions)
		if err != nil {
			we := newError(ErrGraphQLExtensionsDecode, err)
			errs = append(errs, we)
		}
	}

	if len(errs) > 0 {
		return errs
	}

	return nil
}

// Returns a copy of the client with the request modifier set. This allows you to reuse the same
// TCP connection for multiple slightly different requests to the same server
// (i.e. different authentication headers for multitenant applications)
func (c *Client) WithRequestModifier(f RequestModifier) *Client {
	return &Client{
		url:             c.url,
		httpClient:      c.httpClient,
		requestModifier: f,
	}
}

// WithDebug enable debug mode to print internal error detail
func (c *Client) WithDebug(debug bool) *Client {
	return &Client{
		url:             c.url,
		httpClient:      c.httpClient,
		requestModifier: c.requestModifier,
		debug:           debug,
	}
}

// errors represents the "errors" array in a response from a GraphQL server.
// If returned via error interface, the slice is expected to contain at least 1 element.
//
// Specification: https://facebook.github.io/graphql/#sec-Errors.
type Errors []Error

type Error struct {
	Message    string         `json:"message"`
	Extensions map[string]any `json:"extensions"`
	Locations  []struct {
		Line   int `json:"line"`
		Column int `json:"column"`
	} `json:"locations"`
	Path []any `json:"path"`
	err  error
}

// Error implements error interface.
func (e Error) Error() string {
	return fmt.Sprintf("Message: %s, Locations: %+v, Extensions: %+v, Path: %+v", e.Message, e.Locations, e.Extensions, e.Path)
}

// Unwrap implement the unwrap interface.
func (e Error) Unwrap() error {
	return e.err
}

// Error implements error interface.
func (e Errors) Error() string {
	b := strings.Builder{}
	for _, err := range e {
		_, _ = b.WriteString(err.Error())
	}
	return b.String()
}

// Unwrap implements the error unwrap interface.
func (e Errors) Unwrap() []error {
	var errs []error
	for _, err := range e {
		errs = append(errs, err.err)
	}
	return errs
}

func (e Error) getInternalExtension() map[string]any {
	if e.Extensions == nil {
		return make(map[string]any)
	}

	if ex, ok := e.Extensions["internal"]; ok {
		return ex.(map[string]any)
	}

	return make(map[string]any)
}

func newError(code string, err error) Error {
	return Error{
		Message: err.Error(),
		Extensions: map[string]any{
			"code": code,
		},
		err: err,
	}
}

type NetworkError struct {
	body       string
	statusCode int
}

func (e NetworkError) Error() string {
	return fmt.Sprintf("%d %s", e.statusCode, http.StatusText(e.statusCode))
}

func (e NetworkError) Body() string {
	return e.body
}

func (e NetworkError) StatusCode() int {
	return e.statusCode
}

func (e Error) withRequest(req *http.Request, bodyReader io.Reader) Error {
	internal := e.getInternalExtension()
	bodyBytes, err := io.ReadAll(bodyReader)
	if err != nil {
		internal["error"] = err
	} else {
		internal["request"] = map[string]any{
			"headers": req.Header,
			"body":    string(bodyBytes),
		}
	}

	if e.Extensions == nil {
		e.Extensions = make(map[string]any)
	}
	e.Extensions["internal"] = internal
	return e
}

func (e Error) withResponse(res *http.Response, bodyReader io.Reader) Error {
	internal := e.getInternalExtension()

	response := map[string]any{
		"headers": res.Header,
	}

	if bodyReader != nil {
		bodyBytes, err := io.ReadAll(bodyReader)
		if err != nil {
			internal["error"] = err
		} else {
			response["body"] = string(bodyBytes)
		}
	}
	internal["response"] = response
	e.Extensions["internal"] = internal
	return e
}

// UnmarshalGraphQL parses the JSON-encoded GraphQL response data and stores
// the result in the GraphQL query data structure pointed to by v.
//
// The implementation is created on top of the JSON tokenizer available
// in "encoding/json".Decoder.
// This function is re-exported from the internal package
func UnmarshalGraphQL(data []byte, v any) error {
	return jsonutil.UnmarshalGraphQL(data, v)
}

type operationType uint8

const (
	queryOperation operationType = iota
	mutationOperation
	// subscriptionOperation // Unused.

	ErrRequestError            = "request_error"
	ErrJsonEncode              = "json_encode_error"
	ErrJsonDecode              = "json_decode_error"
	ErrGraphQLEncode           = "graphql_encode_error"
	ErrGraphQLDecode           = "graphql_decode_error"
	ErrGraphQLExtensionsDecode = "graphql_extensions_decode_error"
)
