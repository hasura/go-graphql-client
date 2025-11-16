package graphql_test

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/hasura/go-graphql-client"
)

func TestClient_Query_partialDataWithErrorResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustWrite(w, `{
			"data": {
				"node1": {
					"id": "MDEyOklzc3VlQ29tbWVudDE2OTQwNzk0Ng=="
				},
				"node2": null
			},
			"errors": [
				{
					"message": "Could not resolve to a node with the global id of 'NotExist'",
					"type": "NOT_FOUND",
					"path": [
						"node2"
					],
					"locations": [
						{
							"line": 10,
							"column": 4
						}
					]
				}
			]
		}`)
	})
	client := graphql.NewClient(
		"/graphql",
		&http.Client{Transport: localRoundTripper{handler: mux}},
	)

	var q struct {
		Node1 *struct {
			ID graphql.ID
		} `graphql:"node1: node(id: \"MDEyOklzc3VlQ29tbWVudDE2OTQwNzk0Ng==\")"`
		Node2 *struct {
			ID graphql.ID
		} `graphql:"node2: node(id: \"NotExist\")"`
	}

	_, err := client.QueryRaw(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got error: nil, want: non-nil")
	}

	err = client.Query(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got error: nil, want: non-nil")
	}
	if got, want := err.Error(), "Message: Could not resolve to a node with the global id of 'NotExist', Locations: [{Line:10 Column:4}], Extensions: map[], Path: [node2]"; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}

	if q.Node1 == nil || q.Node1.ID != "MDEyOklzc3VlQ29tbWVudDE2OTQwNzk0Ng==" {
		t.Errorf("got wrong q.Node1: %v", q.Node1)
	}
	if q.Node2 != nil {
		t.Errorf("got non-nil q.Node2: %v, want: nil", *q.Node2)
	}
}

func TestClient_Query_partialDataRawQueryWithErrorResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustWrite(w, `{
			"data": {
				"node1": { "id": "MDEyOklzc3VlQ29tbWVudDE2OTQwNzk0Ng==" },
				"node2": null
			},
			"errors": [
				{
					"message": "Could not resolve to a node with the global id of 'NotExist'",
					"type": "NOT_FOUND",
					"path": [
						"node2"
					],
					"locations": [
						{
							"line": 10,
							"column": 4
						}
					]
				}
			]
		}`)
	})
	client := graphql.NewClient(
		"/graphql",
		&http.Client{Transport: localRoundTripper{handler: mux}},
	)

	var q struct {
		Node1 json.RawMessage `graphql:"node1"`
		Node2 *struct {
			ID graphql.ID
		} `graphql:"node2: node(id: \"NotExist\")"`
	}
	err := client.Query(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got error: nil, want: non-nil\n")
	}
	if got, want := err.Error(), "Message: Could not resolve to a node with the global id of 'NotExist', Locations: [{Line:10 Column:4}], Extensions: map[], Path: [node2]"; got != want {
		t.Errorf("got error: %v, want: %v\n", got, want)
	}
	if q.Node1 == nil || string(q.Node1) != `{"id":"MDEyOklzc3VlQ29tbWVudDE2OTQwNzk0Ng=="}` {
		t.Errorf("got wrong q.Node1: %v\n", string(q.Node1))
	}
	if q.Node2 != nil {
		t.Errorf("got non-nil q.Node2: %v, want: nil\n", *q.Node2)
	}

	// test internal error data
	client = client.WithDebug(true)
	err = client.Query(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got error: nil, want: non-nil")
	}
	if !errors.As(err, &graphql.Errors{}) {
		t.Errorf("the error type should be graphql.Errors")
	}

	gqlErr := err.(graphql.Errors)
	if got, want := gqlErr[0].Message, `Could not resolve to a node with the global id of 'NotExist'`; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}
}

func TestClient_Query_noDataWithErrorResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustWrite(w, `{
			"errors": [
				{
					"message": "Field 'user' is missing required arguments: login",
					"locations": [
						{
							"line": 7,
							"column": 3
						}
					]
				}
			]
		}`)
	})
	client := graphql.NewClient(
		"/graphql",
		&http.Client{Transport: localRoundTripper{handler: mux}},
	)

	var q struct {
		User struct {
			Name string
		}
	}
	err := client.Query(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got error: nil, want: non-nil")
	}
	if got, want := err.Error(), "Message: Field 'user' is missing required arguments: login, Locations: [{Line:7 Column:3}], Extensions: map[], Path: []"; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}
	if q.User.Name != "" {
		t.Errorf("got non-empty q.User.Name: %v", q.User.Name)
	}

	_, err = client.QueryRaw(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got error: nil, want: non-nil")
	}

	// test internal error data
	client = client.WithDebug(true)
	err = client.Query(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got error: nil, want: non-nil")
	}
	if !errors.As(err, &graphql.Errors{}) {
		t.Errorf("the error type should be graphql.Errors")
	}

	gqlErr := err.(graphql.Errors)
	if got, want := gqlErr[0].Message, `Field 'user' is missing required arguments: login`; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}

	interErr := gqlErr[0].Extensions["internal"].(map[string]interface{})

	if got, want := interErr["request"].(map[string]interface{})["body"], "{\"query\":\"{user{name}}\"}\n"; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}
}

func TestClient_Query_errorStatusCode(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		http.Error(w, "important message", http.StatusInternalServerError)
	})
	client := graphql.NewClient(
		"/graphql",
		&http.Client{Transport: localRoundTripper{handler: mux}},
	)

	var q struct {
		User struct {
			Name string
		}
	}
	err := client.Query(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got error: nil, want: non-nil")
	}
	if got, want := err.Error(), `Message: 500 Internal Server Error, Locations: [], Extensions: map[code:request_error], Path: []`; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}
	if q.User.Name != "" {
		t.Errorf("got non-empty q.User.Name: %v", q.User.Name)
	}

	gqlErr := err.(graphql.Errors)
	if got, want := gqlErr[0].Extensions["code"], graphql.ErrRequestError; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}
	if _, ok := gqlErr[0].Extensions["internal"]; ok {
		t.Errorf("expected empty internal error")
	}

	// test internal error data
	client = client.WithDebug(true)
	err = client.Query(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got error: nil, want: non-nil")
	}
	if !errors.As(err, &graphql.Errors{}) {
		t.Errorf("the error type should be graphql.Errors")
	}
	gqlErr = err.(graphql.Errors)
	if got, want := gqlErr[0].Message, `500 Internal Server Error`; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}
	if got, want := gqlErr[0].Extensions["code"], graphql.ErrRequestError; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}
	interErr := gqlErr[0].Extensions["internal"].(map[string]interface{})

	if got, want := interErr["request"].(map[string]interface{})["body"], "{\"query\":\"{user{name}}\"}\n"; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}
}

func TestClient_Query_requestError(t *testing.T) {
	want := errors.New("bad error")
	client := graphql.NewClient("/graphql", &http.Client{Transport: errorRoundTripper{err: want}})

	var q struct {
		User struct {
			Name string
		}
	}
	err := client.Query(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got error: nil, want: non-nil")
	}
	if got, want := err.Error(), `Message: Post "/graphql": bad error, Locations: [], Extensions: map[code:request_error], Path: []`; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}
	if q.User.Name != "" {
		t.Errorf("got non-empty q.User.Name: %v", q.User.Name)
	}
	if got := err; !errors.Is(got, want) {
		t.Errorf("got error: %v, want: %v", got, want)
	}

	gqlErr := err.(graphql.Errors)
	if got, want := gqlErr[0].Extensions["code"], graphql.ErrRequestError; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}
	if _, ok := gqlErr[0].Extensions["internal"]; ok {
		t.Errorf("expected empty internal error")
	}
	if got := gqlErr[0]; !errors.Is(err, want) {
		t.Errorf("got error: %v, want %v", got, want)
	}

	// test internal error data
	client = client.WithDebug(true)
	err = client.Query(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got error: nil, want: non-nil")
	}
	if !errors.As(err, &graphql.Errors{}) {
		t.Errorf("the error type should be graphql.Errors")
	}
	gqlErr = err.(graphql.Errors)
	if got, want := gqlErr[0].Message, `Post "/graphql": bad error`; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}
	if got, want := gqlErr[0].Extensions["code"], graphql.ErrRequestError; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}
	interErr := gqlErr[0].Extensions["internal"].(map[string]interface{})

	if got, want := interErr["request"].(map[string]interface{})["body"], "{\"query\":\"{user{name}}\"}\n"; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}
}

// Test that an empty (but non-nil) variables map is
// handled no differently than a nil variables map.
func TestClient_Query_emptyVariables(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		body := mustRead(req.Body)
		if got, want := body, `{"query":"{user{name}}"}`+"\n"; got != want {
			t.Errorf("got body: %v, want %v", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		mustWrite(w, `{"data": {"user": {"name": "Gopher"}}}`)
	})
	client := graphql.NewClient(
		"/graphql",
		&http.Client{Transport: localRoundTripper{handler: mux}},
	)

	var q struct {
		User struct {
			Name string
		}
	}
	err := client.Query(context.Background(), &q, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := q.User.Name, "Gopher"; got != want {
		t.Errorf("got q.User.Name: %q, want: %q", got, want)
	}
}

// Test ignored field
// handled no differently than a nil variables map.
func TestClient_Query_ignoreFields(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		body := mustRead(req.Body)
		if got, want := body, `{"query":"{user{id,name}}"}`+"\n"; got != want {
			t.Errorf("got body: %v, want %v", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		mustWrite(w, `{"data": {"user": {"name": "Gopher"}}}`)
	})
	client := graphql.NewClient(
		"/graphql",
		&http.Client{Transport: localRoundTripper{handler: mux}},
	)

	var q struct {
		User struct {
			ID      string `graphql:"id"`
			Name    string `graphql:"name"`
			Ignored string `graphql:"-"`
		}
	}
	err := client.Query(context.Background(), &q, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := q.User.Name, "Gopher"; got != want {
		t.Errorf("got q.User.Name: %q, want: %q", got, want)
	}
	if got, want := q.User.Ignored, ""; got != want {
		t.Errorf("got q.User.Ignored: %q, want: %q", got, want)
	}
}

// Test raw json response from query
func TestClient_Query_RawResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		body := mustRead(req.Body)
		if got, want := body, `{"query":"{user{id,name}}"}`+"\n"; got != want {
			t.Errorf("got body: %v, want %v", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		mustWrite(w, `{"data": {"user": {"name": "Gopher"}}}`)
	})
	client := graphql.NewClient(
		"/graphql",
		&http.Client{Transport: localRoundTripper{handler: mux}},
	)

	var q struct {
		User struct {
			ID   string `graphql:"id"`
			Name string `graphql:"name"`
		}
	}
	rawBytes, err := client.QueryRaw(context.Background(), &q, map[string]interface{}{})
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	err = json.Unmarshal(rawBytes, &q)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	if got, want := q.User.Name, "Gopher"; got != want {
		t.Errorf("got q.User.Name: %q, want: %q", got, want)
	}
}

// Test exec pre-built query
func TestClient_Exec_Query(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		body := mustRead(req.Body)
		if got, want := body, `{"query":"{user{id,name}}"}`+"\n"; got != want {
			t.Errorf("got body: %v, want %v", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		mustWrite(w, `{"data": {"user": {"name": "Gopher"}}}`)
	})
	client := graphql.NewClient(
		"/graphql",
		&http.Client{Transport: localRoundTripper{handler: mux}},
	)

	var q struct {
		User struct {
			ID   string `graphql:"id"`
			Name string `graphql:"name"`
		}
	}

	err := client.Exec(context.Background(), "{user{id,name}}", &q, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := q.User.Name, "Gopher"; got != want {
		t.Errorf("got q.User.Name: %q, want: %q", got, want)
	}
}

// Test exec pre-built query, return raw json string
func TestClient_Exec_QueryRaw(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		body := mustRead(req.Body)
		if got, want := body, `{"query":"{user{id,name}}"}`+"\n"; got != want {
			t.Errorf("got body: %v, want %v", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		mustWrite(w, `{"data": {"user": {"name": "Gopher"}}}`)
	})
	client := graphql.NewClient(
		"/graphql",
		&http.Client{Transport: localRoundTripper{handler: mux}},
	)

	var q struct {
		User struct {
			ID   string `graphql:"id"`
			Name string `graphql:"name"`
		}
	}

	rawBytes, err := client.ExecRaw(
		context.Background(),
		"{user{id,name}}",
		map[string]interface{}{},
	)
	if err != nil {
		t.Fatal(err)
	}

	err = json.Unmarshal(rawBytes, &q)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := q.User.Name, "Gopher"; got != want {
		t.Errorf("got q.User.Name: %q, want: %q", got, want)
	}
}

func TestClient_BindExtensions(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		body := mustRead(req.Body)
		if got, want := body, `{"query":"{user{id,name}}"}`+"\n"; got != want {
			t.Errorf("got body: %v, want %v", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		mustWrite(
			w,
			`{"data": {"user": {"name": "Gopher"}}, "extensions": {"id": 1, "domain": "users"}}`,
		)
	})
	client := graphql.NewClient(
		"/graphql",
		&http.Client{Transport: localRoundTripper{handler: mux}},
	)

	var q struct {
		User struct {
			ID   string `graphql:"id"`
			Name string `graphql:"name"`
		}
	}

	var ext struct {
		ID     int    `json:"id"`
		Domain string `json:"domain"`
	}

	err := client.Query(context.Background(), &q, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := q.User.Name, "Gopher"; got != want {
		t.Fatalf("got q.User.Name: %q, want: %q", got, want)
	}

	headers := http.Header{}
	err = client.Query(
		context.Background(),
		&q,
		map[string]interface{}{},
		graphql.BindExtensions(&ext),
		graphql.BindResponseHeaders(&headers),
	)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := q.User.Name, "Gopher"; got != want {
		t.Fatalf("got q.User.Name: %q, want: %q", got, want)
	}

	if got, want := ext.ID, 1; got != want {
		t.Errorf("got ext.ID: %q, want: %q", got, want)
	}
	if got, want := ext.Domain, "users"; got != want {
		t.Errorf("got ext.Domain: %q, want: %q", got, want)
	}

	if len(headers) != 1 {
		t.Error("got empty headers, want 1")
	}

	if got, want := headers.Get("content-type"), "application/json"; got != want {
		t.Errorf("got headers[content-type]: %q, want: %s", got, want)
	}
}

// Test exec pre-built query, return raw json string and map
// with extensions
func TestClient_Exec_QueryRawWithExtensions(t *testing.T) {
	testResponseHeader := "X-Test-Response"
	testResponseHeaderValue := "graphql"

	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		body := mustRead(req.Body)
		if got, want := body, `{"query":"{user{id,name}}"}`+"\n"; got != want {
			t.Errorf("got body: %v, want %v", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set(testResponseHeader, testResponseHeaderValue)
		mustWrite(
			w,
			`{"data": {"user": {"name": "Gopher"}}, "extensions": {"id": 1, "domain": "users"}}`,
		)
	})
	client := graphql.NewClient(
		"/graphql",
		&http.Client{Transport: localRoundTripper{handler: mux}},
	)

	var ext struct {
		ID     int    `json:"id"`
		Domain string `json:"domain"`
	}

	headers := http.Header{}
	_, extensions, err := client.ExecRawWithExtensions(
		context.Background(),
		"{user{id,name}}",
		map[string]interface{}{},
		graphql.BindResponseHeaders(&headers),
	)
	if err != nil {
		t.Fatal(err)
	}

	if got := extensions; got == nil {
		t.Errorf("got nil extensions: %q, want: non-nil", got)
	}

	err = json.Unmarshal(extensions, &ext)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := ext.ID, 1; got != want {
		t.Errorf("got ext.ID: %q, want: %q", got, want)
	}
	if got, want := ext.Domain, "users"; got != want {
		t.Errorf("got ext.Domain: %q, want: %q", got, want)
	}

	if len(headers) != 2 {
		t.Error("got empty headers, want 2")
	}

	if headerValue := headers.Get(testResponseHeader); headerValue != testResponseHeaderValue {
		t.Errorf(
			"got headers[%s]: %q, want: %s",
			testResponseHeader,
			headerValue,
			testResponseHeaderValue,
		)
	}
}

// Issue: https://github.com/hasura/go-graphql-client/issues/139
func TestUnmarshalGraphQL_unionSlice(t *testing.T) {
	expectedQuery := `query($cursor0: String, $searchQuery: String!) {
		search(type: ISSUE, query: $searchQuery, first: 100, after: $cursor0) {
			pageInfo {
				endCursor
				hasNextPage
			}
			nodes {
				... on Issue {
					number
					id
					createdAt
					updatedAt
					state
					stateReason
					closedAt
					author {
						login
					}
					authorAssociation
					title
					body
				}
			}
		}`

	type PageInfo struct {
		EndCursor   string
		HasNextPage bool
	}

	type issue struct {
		Number      int64
		ID          string
		CreatedAt   time.Time
		UpdatedAt   time.Time
		State       string
		StateReason *string
		ClosedAt    *time.Time
		Author      struct {
			Login string
		}
		AuthorAssociation string
		Title             string
		Body              string
	}

	type querySearch struct {
		PageInfo PageInfo
		Nodes    []issue
	}

	type queryIssues struct {
		// results
		Search querySearch
	}

	want := queryIssues{
		Search: querySearch{
			PageInfo: PageInfo{
				EndCursor:   "Y3Vyc29yOjE=",
				HasNextPage: false,
			},
			Nodes: []issue{
				{
					Number:    32,
					ID:        "I_kwDOJBmYg86J6maD",
					CreatedAt: time.Date(2024, 05, 23, 21, 03, 9, 0, time.UTC),
					UpdatedAt: time.Date(2024, 05, 28, 15, 42, 2, 0, time.UTC),
					State:     "OPEN",
					Author: struct{ Login string }{
						Login: "drewAdorno",
					},
					AuthorAssociation: "NONE",
					Title:             "subdomains not redirecting",
					Body:              "Hi,\r\nI was able to configure and successfully use localias in was wsl env (ubuntu 22.04)",
				},
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustWrite(w, `{
			"data": {
				"search": {
					"pageInfo": {
						"endCursor": "Y3Vyc29yOjE=",
						"hasNextPage": false
					},
					"nodes": [
						{
							"number": 32,
							"id": "I_kwDOJBmYg86J6maD",
							"createdAt": "2024-05-23T21:03:09Z",
							"updatedAt": "2024-05-28T15:42:02Z",
							"state": "OPEN",
							"stateReason": null,
							"closedAt": null,
							"author": {
								"login": "drewAdorno"
							},
							"authorAssociation": "NONE",
							"title": "subdomains not redirecting",
							"body": "Hi,\r\nI was able to configure and successfully use localias in was wsl env (ubuntu 22.04)"
						}
					]
				}
			}
		}`)
	})
	client := graphql.NewClient("/graphql", &http.Client{Transport: localRoundTripper{handler: mux}})

	var got queryIssues
	err := client.Exec(context.Background(), expectedQuery, &got, map[string]interface{}{
		"searchQuery": "test",
		"cursor0":     "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(want, got) {
		t.Errorf("got %+v, want: %+v", got, want)
	}
}

// Issue: https://github.com/hasura/go-graphql-client/issues/152
func TestUnmarshalGraphQL_shopifyAdminAPI(t *testing.T) {

	type testQuery struct {
		Orders struct {
			Edges []struct {
				Cursor string
				Node   struct {
					ID                       string
					Test                     bool
					Name                     string
					Email                    *string
					DisplayFinancialStatus   string
					DisplayFulfillmentStatus string
					ReturnStatus             string
					Note                     string
					ClientIP                 string
					Closed                   bool
					ClosedAt                 *time.Time
					CancelledAt              *time.Time
					CustomAttributes         []struct {
						Key   string
						Value string
					}
					Customer struct {
						Email string
					}
				}
			}
			PageInfo struct {
				EndCursor   string
				HasNextPage bool
			}
		} `graphql:"orders(first: $first, after: $after)"`
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mustWrite(w, `{
			"data": {
				"orders": {
					"edges": [
						{
							"cursor": "x==",
							"node": {
								"id": "gid://shopify/Order/x",
								"test": false,
								"name": "#1004",
								"email": null,
								"displayFinancialStatus": "PAID",
								"displayFulfillmentStatus": "UNFULFILLED",
								"returnStatus": "NO_RETURN",
								"note": "unfulfilled",
								"clientIp": "x",
								"closed": false,
								"closedAt": null,
								"cancelledAt": null,
								"customAttributes": [],
								"customer": null
							}
						},
						{
							"cursor": "x==",
							"node": {
								"id": "gid://shopify/Order/x",
								"test": false,
								"name": "#1005",
								"email": null,
								"displayFinancialStatus": "REFUNDED",
								"displayFulfillmentStatus": "FULFILLED",
								"returnStatus": "NO_RETURN",
								"note": "fulfilled then refunded (not returned)",
								"clientIp": "x",
								"closed": true,
								"closedAt": "2024-08-29T02:07:04Z",
								"cancelledAt": null,
								"customAttributes": [],
								"customer": null
							}
						},
						{
							"cursor": "x==",
							"node": {
								"id": "gid://shopify/Order/x",
								"test": false,
								"name": "#1006",
								"email": null,
								"displayFinancialStatus": "PAID",
								"displayFulfillmentStatus": "FULFILLED",
								"returnStatus": "IN_PROGRESS",
								"note": "fulfulled and return in progress",
								"clientIp": "x",
								"closed": false,
								"closedAt": null,
								"cancelledAt": null,
								"customAttributes": [],
								"customer": null
							}
						}
					],
					"pageInfo": {
						"endCursor": "x==",
						"hasNextPage": false
					}
				}
			},
			"extensions": {
				"cost": {
					"requestedQueryCost": 8,
					"actualQueryCost": 8,
					"throttleStatus": {
						"maximumAvailable": 2000.0,
						"currentlyAvailable": 1992,
						"restoreRate": 100.0
					}
				}
			}
		}`)
	})
	client := graphql.NewClient("/graphql", &http.Client{Transport: localRoundTripper{handler: mux}})

	var got testQuery

	err := client.Query(context.Background(), &got, map[string]interface{}{
		"first": 10,
		"after": "test",
	})
	if err != nil {
		t.Fatal(err)
	}
}

// localRoundTripper is an http.RoundTripper that executes HTTP transactions
// by using handler directly, instead of going over an HTTP connection.
type localRoundTripper struct {
	handler http.Handler
}

func (l localRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	l.handler.ServeHTTP(w, req)
	return w.Result(), nil
}

// errorRoundTripper is an http.RoundTripper that always returns the supplied
// error.
type errorRoundTripper struct {
	err error
}

func (e errorRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, e.err
}

func mustRead(r io.Reader) string {
	b, err := io.ReadAll(r)
	if err != nil {
		panic(err)
	}

	return string(b)
}

func mustWrite(w io.Writer, s string) {
	_, err := io.WriteString(w, s)
	if err != nil {
		panic(err)
	}
}

func TestClientOption_WithRetry_succeed(t *testing.T) {
	var (
		attempts    int
		maxAttempts = 3
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		attempts++
		// Simulate a temporary network error except for the last attempt
		if attempts <= maxAttempts-1 {
			http.Error(w, "temporary error", http.StatusServiceUnavailable)
			return
		}
		// Succeed on the last attempt
		w.Header().Set("Content-Type", "application/json")
		mustWrite(w, `{"data": {"user": {"name": "Gopher"}}}`)
	})

	client := graphql.NewClient("/graphql",
		&http.Client{
			Transport: localRoundTripper{
				handler: mux,
			},
		},
		graphql.WithRetry(maxAttempts-1),
	)

	var q struct {
		User struct {
			Name string
		}
	}

	err := client.Query(context.Background(), &q, nil)
	if err != nil {
		t.Fatalf("got error: %v, want nil", err)
	}

	if attempts != maxAttempts {
		t.Errorf("got %d attempts, want %d", attempts, maxAttempts)
	}

	if q.User.Name != "Gopher" {
		t.Errorf("got q.User.Name: %q, want Gopher", q.User.Name)
	}
}

func TestClientOption_WithRetry_maxRetriesExceeded(t *testing.T) {
	var (
		attempts             int
		maxAttempts          = 3
		retryBase            = time.Second / 2
		retryExponentialRate = 2.0
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		attempts++
		// Always fail with a temporary error
		http.Error(w, "temporary error", http.StatusServiceUnavailable)
	})

	client := graphql.NewClient("/graphql",
		&http.Client{
			Transport: localRoundTripper{
				handler: mux,
			},
		},
		graphql.WithRetry(maxAttempts-1),
		graphql.WithRetryBaseDelay(retryBase),
		graphql.WithRetryExponentialRate(retryExponentialRate),
		graphql.WithRetryHTTPStatus([]int{http.StatusServiceUnavailable}),
	)

	var q struct {
		User struct {
			Name string
		}
	}

	start := time.Now()
	err := client.Query(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got nil, want error")
	}

	latency := time.Now().Sub(start)
	// Check that we got the max retries exceeded error
	var gqlErrs graphql.Errors
	if !errors.As(err, &gqlErrs) {
		t.Fatalf("got %T, want graphql.Errors", err)
	}

	if len(gqlErrs) != 1 {
		t.Fatalf("got %d, want 1 error", len(gqlErrs))
	}

	// First request does not count
	if attempts != maxAttempts {
		t.Errorf("got %d attempts, want %d", attempts, maxAttempts)
	}

	expectedLatency := time.Duration(1.5 * float64(time.Second))
	expectedLatencyMax := expectedLatency + time.Duration(0.5*float64(time.Second))
	if latency < expectedLatency || latency > expectedLatencyMax {
		t.Errorf(
			"latency must be in between %s < %s, got %s",
			expectedLatency,
			expectedLatencyMax,
			latency,
		)
	}
}

func TestClientOption_WithRetryAfter(t *testing.T) {
	var (
		attempts    int
		maxAttempts = 2
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		attempts++
		w.Header().Add("Retry-After", "2")

		// Always fail with a temporary error
		http.Error(w, "temporary error", http.StatusServiceUnavailable)
	})

	client := graphql.NewClient("/graphql",
		&http.Client{
			Transport: localRoundTripper{
				handler: mux,
			},
		},
		graphql.WithRetry(maxAttempts-1),
	)

	var q struct {
		User struct {
			Name string
		}
	}

	start := time.Now()
	err := client.Query(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got nil, want error")
	}

	latency := time.Now().Sub(start)
	// Check that we got the max retries exceeded error
	var gqlErrs graphql.Errors
	if !errors.As(err, &gqlErrs) {
		t.Fatalf("got %T, want graphql.Errors", err)
	}

	if len(gqlErrs) != 1 {
		t.Fatalf("got %d, want 1 error", len(gqlErrs))
	}

	// First request does not count
	if attempts != maxAttempts {
		t.Errorf("got %d attempts, want %d", attempts, maxAttempts)
	}

	expectedLatency := time.Duration(2 * time.Second)
	expectedLatencyMax := expectedLatency + time.Duration(0.5*float64(time.Second))
	if latency < expectedLatency || latency > expectedLatencyMax {
		t.Errorf(
			"latency must be in between %s < %s, got %s",
			expectedLatency,
			expectedLatencyMax,
			latency,
		)
	}
}

// Define the custom RoundTripper type
type roundTripperWithRetryCount struct {
	Transport *http.Transport
	attempts  *int
}

// Define RoundTrip method for the type
func (c roundTripperWithRetryCount) RoundTrip(req *http.Request) (*http.Response, error) {
	if c.attempts != nil {
		*c.attempts++
	}
	return c.Transport.RoundTrip(req)
}

func TestClientOption_WithRetry_shouldNotRetry(t *testing.T) {
	var attempts int

	client := graphql.NewClient("/graphql",
		&http.Client{
			Transport: roundTripperWithRetryCount{
				attempts: &attempts,
				Transport: &http.Transport{
					DialContext: (&net.Dialer{
						Timeout:   3 * time.Second,
						KeepAlive: 3 * time.Second,
					}).DialContext,
				},
			},
		},
		graphql.WithRetry(3),
	)

	var q struct {
		User struct {
			Name string
		}
	}

	err := client.Query(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got nil, want error")
	}

	// Should not retry on permanent URL errors
	if attempts != 1 {
		t.Errorf("got %d attempts, want 1", attempts)
	}
}

func TestClient_Query_retryGraphQLError(t *testing.T) {
	var (
		attempts    int
		maxAttempts = 3
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "application/json")
		mustWrite(w, `{
			"errors": [
				{
					"message": "Field 'user' is missing required arguments: login",
					"locations": [
						{
							"line": 7,
							"column": 3
						}
					]
				}
			]
		}`)
	})

	client := graphql.NewClient(
		"/graphql",
		&http.Client{Transport: localRoundTripper{handler: mux}},
		graphql.WithRetry(maxAttempts-1),
		graphql.WithRetryOnGraphQLError(func(errs graphql.Errors) bool {
			return len(errs) == 1 &&
				errs[0].Message == "Field 'user' is missing required arguments: login"
		}),
	)

	var q struct {
		User struct {
			Name string
		}
	}

	err := client.Query(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got error: nil, want: non-nil")
	}

	if got, want := err.Error(), "Message: Field 'user' is missing required arguments: login, Locations: [{Line:7 Column:3}], Extensions: map[], Path: []"; got != want {
		t.Errorf("got error: %v, want: %v", got, want)
	}
	if q.User.Name != "" {
		t.Errorf("got non-empty q.User.Name: %v", q.User.Name)
	}

	if attempts != maxAttempts {
		t.Errorf("got %d attempts, want %d", attempts, maxAttempts)
	}
}

func TestClient_Query_Compression(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")

		gw := gzip.NewWriter(w)
		_, err := gw.Write([]byte(`{"data": {"user": {"name": "Gopher"}}}`))
		if err != nil {
			t.Fatal(err)
		}

		gw.Close()
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := graphql.NewClient(server.URL+"/graphql", http.DefaultClient)

	var q struct {
		User struct {
			Name string
		}
	}

	err := client.Query(context.Background(), &q, nil)
	if err != nil {
		t.Fatalf("want: nil, got: %s", err)
	}

	if q.User.Name != "Gopher" {
		t.Errorf("got invalid q.User.Name: %v, want: Gopher", q.User.Name)
	}
}

func TestClient_WithRequestModifier_DoesNotChangeOtherFields(t *testing.T) {
	client := graphql.NewClient(
		"http://example.com/graphql",
		&http.Client{Timeout: 10 * time.Second},
		graphql.WithRetry(3),
		graphql.WithRetryBaseDelay(2*time.Second),
	)

	requestModifier := func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer test-token")
	}

	clientAfterWithRequestModifier := client.WithRequestModifier(requestModifier).WithRequestModifier(nil)

	if got, want := clientAfterWithRequestModifier, client; !reflect.DeepEqual(got, want) {
		t.Errorf("got changed fields after WithRequestModifier: %v, want: %v", got, want)
	}
}

func TestClient_WithDebug_DoesNotChangeOtherFields(t *testing.T) {
	client := graphql.NewClient(
		"http://example.com/graphql",
		&http.Client{Timeout: 10 * time.Second},
		graphql.WithRetry(3),
		graphql.WithRetryBaseDelay(2*time.Second),
	)

	clientAfterWithDebug := client.WithDebug(true).WithDebug(false)

	if got, want := clientAfterWithDebug, client; !reflect.DeepEqual(got, want) {
		t.Errorf("got changed fields after WithDebug: %v, want: %v", got, want)
	}
}
