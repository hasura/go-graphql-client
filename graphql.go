package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hasura/go-graphql-client/pkg/jsonutil"
)

// Doer interface has the method required to use a type as custom http client.
// The net/*http.Client type satisfies this interface.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// headers  amongst other things.
type RequestModifier func(*http.Request)

// Client is a GraphQL client.
type Client struct {
	url             string // GraphQL server URL.
	httpClient      Doer
	requestModifier RequestModifier
	debug           bool
	// max number of retry times, defaults to 0 for no retry
	maxRetries int
	// base delay between retries, default 1 second
	retryBaseDelay time.Duration
	// retry exponential rate, default 2.0
	retryExponentialRate float64
	// only retry on the following http statuses
	retryHttpStatus []int
	// set the callback to retry on specific graphql errors
	retryOnGraphQLError func(errs Errors) bool
}

// NewClient creates a GraphQL client targeting the specified GraphQL server URL.
// If httpClient is nil, then http.DefaultClient is used.
func NewClient(url string, httpClient Doer, options ...ClientOption) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	c := &Client{
		url:                  url,
		httpClient:           httpClient,
		requestModifier:      nil,
		retryBaseDelay:       time.Second,
		retryExponentialRate: 2,
		retryHttpStatus: []int{
			http.StatusTooManyRequests,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout,
		},
	}

	for _, opt := range options {
		opt(c)
	}

	return c
}

// Query executes a single GraphQL query request,
// with a query derived from q, populating the response into it.
// q should be a pointer to struct that corresponds to the GraphQL schema.
func (c *Client) Query(
	ctx context.Context,
	q any,
	variables map[string]any,
	options ...Option,
) error {
	return c.do(ctx, queryOperation, q, variables, options...)
}

// Deprecated: this is the shortcut of Query method, with NewOperationName option.
func (c *Client) NamedQuery(
	ctx context.Context,
	name string,
	q any,
	variables map[string]any,
	options ...Option,
) error {
	return c.do(ctx, queryOperation, q, variables, append(options, OperationName(name))...)
}

// Mutate executes a single GraphQL mutation request,
// with a mutation derived from m, populating the response into it.
// m should be a pointer to struct that corresponds to the GraphQL schema.
func (c *Client) Mutate(
	ctx context.Context,
	m any,
	variables map[string]any,
	options ...Option,
) error {
	return c.do(ctx, mutationOperation, m, variables, options...)
}

// Deprecated: this is the shortcut of Mutate method, with NewOperationName option.
func (c *Client) NamedMutate(
	ctx context.Context,
	name string,
	m any,
	variables map[string]any,
	options ...Option,
) error {
	return c.do(ctx, mutationOperation, m, variables, append(options, OperationName(name))...)
}

// Query executes a single GraphQL query request,
// with a query derived from q, populating the response into it.
// q should be a pointer to struct that corresponds to the GraphQL schema.
// return raw bytes message.
func (c *Client) QueryRaw(
	ctx context.Context,
	q any,
	variables map[string]any,
	options ...Option,
) ([]byte, error) {
	return c.doRaw(ctx, queryOperation, q, variables, options...)
}

// NamedQueryRaw executes a single GraphQL query request, with operation name
// return raw bytes message.
func (c *Client) NamedQueryRaw(
	ctx context.Context,
	name string,
	q any,
	variables map[string]any,
	options ...Option,
) ([]byte, error) {
	return c.doRaw(ctx, queryOperation, q, variables, append(options, OperationName(name))...)
}

// MutateRaw executes a single GraphQL mutation request,
// with a mutation derived from m, populating the response into it.
// m should be a pointer to struct that corresponds to the GraphQL schema.
// return raw bytes message.
func (c *Client) MutateRaw(
	ctx context.Context,
	m any,
	variables map[string]any,
	options ...Option,
) ([]byte, error) {
	return c.doRaw(ctx, mutationOperation, m, variables, options...)
}

// NamedMutateRaw executes a single GraphQL mutation request, with operation name
// return raw bytes message.
func (c *Client) NamedMutateRaw(
	ctx context.Context,
	name string,
	m any,
	variables map[string]any,
	options ...Option,
) ([]byte, error) {
	return c.doRaw(ctx, mutationOperation, m, variables, append(options, OperationName(name))...)
}

// buildQueryAndOptions the common method to build query and options.
func (c *Client) buildQueryAndOptions(
	op operationType,
	v any,
	variables map[string]any,
	options ...Option,
) (string, *constructOptionsOutput, error) {
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

// execute the http request with backoff retries.
func (c *Client) doHttpRequest(ctx context.Context, body io.ReadSeeker) *rawGraphQLResult {
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		_, _ = body.Seek(0, io.SeekStart)

		request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, body)
		if err != nil {
			e := newError(ErrRequestError, fmt.Errorf("problem constructing request: %w", err))
			if c.debug {
				_, _ = body.Seek(0, 0)
				e = e.withRequest(request, body)
			}

			return &rawGraphQLResult{
				Errors: Errors{e},
			}
		}

		request.Header.Add("Content-Type", "application/json")

		if c.requestModifier != nil {
			c.requestModifier(request)
		}

		resp, err := c.httpClient.Do(request)
		if err != nil {
			e := newError(ErrRequestError, err)

			if c.debug {
				_, _ = body.Seek(0, io.SeekStart)
				e = e.withRequest(request, body)
			}

			return &rawGraphQLResult{
				Errors: Errors{e},
			}
		}

		defer func() {
			_ = resp.Body.Close()
		}()

		switch {
		case resp.StatusCode >= 400:
			if attempt >= c.maxRetries || !c.shouldRetryHttpError(resp) {
				errorMessage, err := io.ReadAll(resp.Body)
				if err != nil {
					errorMessage = []byte(resp.Status)
				}

				gqlError := newError(ErrRequestError, NetworkError{
					statusCode: resp.StatusCode,
					body:       string(errorMessage),
				})

				if c.debug {
					_, _ = body.Seek(0, io.SeekStart)
					gqlError = gqlError.withRequest(request, body)
				}

				return &rawGraphQLResult{
					Errors: Errors{gqlError},
				}
			}
		case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated:
			resp := c.decodeRawGraphQLResponse(request, body, resp)
			if len(resp.Errors) == 0 || !resp.decoded {
				return resp
			}

			if attempt >= c.maxRetries || c.retryOnGraphQLError == nil ||
				!c.retryOnGraphQLError(resp.Errors) {
				if c.debug &&
					(resp.Errors[0].Extensions == nil || resp.Errors[0].Extensions["request"] == nil) {
					_, _ = body.Seek(0, io.SeekStart)

					if resp != nil && resp.responseBody != nil {
						_, _ = resp.responseBody.Seek(0, io.SeekStart)
					}

					resp.Errors[0] = resp.Errors[0].
						withRequest(request, body).
						withResponse(resp.response, resp.responseBody)
				}

				return resp
			}
		default:
			return &rawGraphQLResult{
				Errors: Errors{
					newError(
						ErrRequestError,
						fmt.Errorf("invalid HTTP status code: %d %s", resp.StatusCode, resp.Status),
					),
				},
			}
		}

		time.Sleep(c.getRetryDelay(resp, attempt))
	}

	return &rawGraphQLResult{
		Errors: Errors{newError(ErrRequestError, errors.New("unreachable code"))},
	}
}

// The HTTP [Retry-After] response header indicates how long the user agent should wait before making a follow-up request.
// The client finds this header if exist and decodes to duration.
// If the header doesn't exist or there is any error happened, fallback to the retry delay setting.
//
// [Retry-After]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Retry-After
func (c *Client) getRetryDelay(resp *http.Response, attempts int) time.Duration {
	if rawRetryAfter := resp.Header.Get("Retry-After"); rawRetryAfter != "" {
		// A non-negative decimal integer indicating the seconds to delay after the response is received.
		retryAfterSecs, err := strconv.ParseInt(rawRetryAfter, 10, 32)
		if err == nil && retryAfterSecs > 0 {
			return time.Duration(
				math.Max(float64(int64(time.Second)*retryAfterSecs), float64(c.retryBaseDelay)),
			)
		}

		// A date after which to retry, e.g. Tue, 29 Oct 2024 16:56:32 GMT
		retryTime, err := time.Parse(time.RFC1123, rawRetryAfter)
		if err == nil && retryTime.After(time.Now()) {
			duration := time.Until(retryTime)

			return time.Duration(math.Max(float64(duration), float64(c.retryBaseDelay)))
		}
	}

	return time.Duration(
		float64(c.retryBaseDelay) * math.Pow(c.retryExponentialRate, float64(attempts)),
	)
}

func (c *Client) decodeRawGraphQLResponse(
	req *http.Request,
	reqBody io.ReadSeeker,
	resp *http.Response,
) *rawGraphQLResult {
	var r io.Reader = resp.Body

	// copy the response reader for debugging
	var respReader *bytes.Reader

	if c.debug {
		body, err := io.ReadAll(r)
		if err != nil {
			return &rawGraphQLResult{
				Errors: Errors{newError(ErrJsonDecode, err)},
			}
		}

		respReader = bytes.NewReader(body)
		r = respReader
	}

	var out rawGraphQLResult

	err := json.NewDecoder(r).Decode(&out)
	out.request = req
	out.requestBody = reqBody
	out.response = resp
	out.responseBody = respReader

	if err != nil {
		we := newError(ErrJsonDecode, err)

		if c.debug {
			_, _ = reqBody.Seek(0, io.SeekStart)
			_, _ = respReader.Seek(0, io.SeekStart)
			we = we.withRequest(req, reqBody).
				withResponse(resp, respReader)
		}

		out.Errors = Errors{we}

		return &out
	}

	out.decoded = true

	return &out
}

// shouldRetryHttpError determines whether the request should be retried by the http status code.
func (c *Client) shouldRetryHttpError(resp *http.Response) bool {
	for _, status := range c.retryHttpStatus {
		if status == resp.StatusCode {
			return true
		}
	}

	return false
}

// doRequest sends graphql request.
func (c *Client) doRequest(
	ctx context.Context,
	query string,
	variables map[string]any,
	options *constructOptionsOutput,
) *rawGraphQLResult {
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
		return &rawGraphQLResult{
			Errors: Errors{newError(ErrGraphQLEncode, err)},
		}
	}

	reqReader := bytes.NewReader(buf.Bytes())

	resp := c.doHttpRequest(ctx, reqReader)

	if options != nil && options.headers != nil && resp.response != nil {
		for key, values := range resp.response.Header {
			for _, value := range values {
				options.headers.Add(key, value)
			}
		}
	}

	if len(resp.Data) == 0 {
		resp.Data = nil
	}

	if len(resp.Extensions) == 0 {
		resp.Extensions = nil
	}

	return resp
}

// return raw message and error.
func (c *Client) doRaw(
	ctx context.Context,
	op operationType,
	v any,
	variables map[string]any,
	options ...Option,
) ([]byte, error) {
	query, optionsOutput, err := c.buildQueryAndOptions(op, v, variables, options...)
	if err != nil {
		return nil, err
	}

	resp := c.doRequest(ctx, query, variables, optionsOutput)
	if len(resp.Errors) > 0 {
		return resp.Data, resp.Errors
	}

	return resp.Data, nil
}

// do executes a single GraphQL operation and unmarshal json.
func (c *Client) do(
	ctx context.Context,
	op operationType,
	v any,
	variables map[string]any,
	options ...Option,
) error {
	query, optionsOutput, err := c.buildQueryAndOptions(op, v, variables, options...)
	if err != nil {
		return err
	}

	resp := c.doRequest(ctx, query, variables, optionsOutput)

	return c.processResponse(v, resp, optionsOutput.extensions)
}

// Executes a pre-built query and unmarshals the response into v. Unlike the Query method you have to specify in the query the
// fields that you want to receive as they are not inferred from v. This method is useful if you need to build the query dynamically.
func (c *Client) Exec(
	ctx context.Context,
	query string,
	v any,
	variables map[string]any,
	options ...Option,
) error {
	optionsOutput, err := constructOptions(options)
	if err != nil {
		return err
	}

	resp := c.doRequest(ctx, query, variables, optionsOutput)

	return c.processResponse(v, resp, optionsOutput.extensions)
}

// Executes a pre-built query and returns the raw json message. Unlike the Query method you have to specify in the query the
// fields that you want to receive as they are not inferred from the interface. This method is useful if you need to build the query dynamically.
func (c *Client) ExecRaw(
	ctx context.Context,
	query string,
	variables map[string]any,
	options ...Option,
) ([]byte, error) {
	optionsOutput, err := constructOptions(options)
	if err != nil {
		return nil, err
	}

	resp := c.doRequest(ctx, query, variables, optionsOutput)
	if len(resp.Errors) > 0 {
		return resp.Data, resp.Errors
	}

	return resp.Data, nil
}

// ExecRawWithExtensions execute a pre-built query and returns the raw json message and a map with extensions (values also as raw json objects). Unlike the
// Query method you have to specify in the query the fields that you want to receive as they are not inferred from the interface. This method
// is useful if you need to build the query dynamically.
func (c *Client) ExecRawWithExtensions(
	ctx context.Context,
	query string,
	variables map[string]any,
	options ...Option,
) ([]byte, []byte, error) {
	optionsOutput, err := constructOptions(options)
	if err != nil {
		return nil, nil, err
	}

	resp := c.doRequest(ctx, query, variables, optionsOutput)
	if len(resp.Errors) > 0 {
		return resp.Data, resp.Extensions, resp.Errors
	}

	return resp.Data, resp.Extensions, nil
}

func (c *Client) processResponse(v any, resp *rawGraphQLResult, extensions any) error {
	errs := resp.Errors

	if len(resp.Data) > 0 {
		err := jsonutil.UnmarshalGraphQL(resp.Data, v)
		if err != nil {
			we := newError(ErrGraphQLDecode, err)

			if c.debug {
				we = we.withResponse(resp.response, resp.responseBody)
			}

			errs = append(errs, we)
		}
	}

	if len(resp.Extensions) > 0 && extensions != nil {
		err := json.Unmarshal(resp.Extensions, extensions)
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

// (i.e. different authentication headers for multitenant applications).
func (c *Client) WithRequestModifier(f RequestModifier) *Client {
	return &Client{
		url:             c.url,
		httpClient:      c.httpClient,
		requestModifier: f,
		debug:           c.debug,
	}
}

// WithDebug enable debug mode to print internal error detail.
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
	return fmt.Sprintf(
		"Message: %s, Locations: %+v, Extensions: %+v, Path: %+v",
		e.Message,
		e.Locations,
		e.Extensions,
		e.Path,
	)
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
	errs := make([]error, len(e))
	for i, err := range e {
		errs[i] = err.err
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

// This function is re-exported from the internal package.
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

type rawGraphQLResult struct {
	Data       json.RawMessage `json:"data"`
	Extensions json.RawMessage `json:"extensions"`
	Errors     Errors          `json:"errors"`

	// request and response information
	decoded      bool
	request      *http.Request
	requestBody  io.ReadSeeker
	response     *http.Response
	responseBody io.ReadSeeker
}
