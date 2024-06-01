package graphql_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	client := graphql.NewClient("/graphql", &http.Client{Transport: localRoundTripper{handler: mux}})

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
	client := graphql.NewClient("/graphql", &http.Client{Transport: localRoundTripper{handler: mux}})

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
	client := graphql.NewClient("/graphql", &http.Client{Transport: localRoundTripper{handler: mux}})

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
	client := graphql.NewClient("/graphql", &http.Client{Transport: localRoundTripper{handler: mux}})

	var q struct {
		User struct {
			Name string
		}
	}
	err := client.Query(context.Background(), &q, nil)
	if err == nil {
		t.Fatal("got error: nil, want: non-nil")
	}
	if got, want := err.Error(), `Message: 500 Internal Server Error; body: "important message\n", Locations: [], Extensions: map[code:request_error], Path: []`; got != want {
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
	if got, want := gqlErr[0].Message, `500 Internal Server Error; body: "important message\n"`; got != want {
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
	client := graphql.NewClient("/graphql", &http.Client{Transport: localRoundTripper{handler: mux}})

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
	client := graphql.NewClient("/graphql", &http.Client{Transport: localRoundTripper{handler: mux}})

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
	client := graphql.NewClient("/graphql", &http.Client{Transport: localRoundTripper{handler: mux}})

	var q struct {
		User struct {
			ID   string `graphql:"id"`
			Name string `graphql:"name"`
		}
	}
	rawBytes, err := client.QueryRaw(context.Background(), &q, map[string]interface{}{})
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
	client := graphql.NewClient("/graphql", &http.Client{Transport: localRoundTripper{handler: mux}})

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
	client := graphql.NewClient("/graphql", &http.Client{Transport: localRoundTripper{handler: mux}})

	var q struct {
		User struct {
			ID   string `graphql:"id"`
			Name string `graphql:"name"`
		}
	}

	rawBytes, err := client.ExecRaw(context.Background(), "{user{id,name}}", map[string]interface{}{})
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
