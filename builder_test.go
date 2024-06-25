package graphql_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hasura/go-graphql-client"
)

func TestBuilder(t *testing.T) {

	queryStruct1 := func() interface{} {
		m := struct {
			ID   string
			Name string
		}{}
		return &m
	}

	queryStruct2 := func() interface{} {
		m := struct {
			ID        string
			Email     string
			Addresses []struct {
				Street string
				City   string
			}
		}{}
		return &m
	}

	queryStruct3 := func() interface{} {
		m := struct {
			ID     string
			Email  string
			Points []string
		}{}
		return &m
	}

	fixtures := []struct {
		queries            map[string]interface{}
		variables          map[string]interface{}
		variableMismatched bool
		err                error
	}{
		{
			queries: map[string]interface{}{
				"person":        queryStruct1(),
				"personAddress": queryStruct2(),
				"personPoint":   queryStruct3(),
			},
		},
		{
			queries: map[string]interface{}{
				"person(id: $id)":               queryStruct1(),
				"personAddress(id: $addressId)": queryStruct2(),
				"personPoint(where: $where)":    queryStruct3(),
			},
			variables: map[string]interface{}{
				"id":        nil,
				"addressId": nil,
				"where":     nil,
			},
		},
		{
			err: errors.New("no graphql query to be built"),
		},
		{
			queries: map[string]interface{}{
				"person(id: $id)":               queryStruct1(),
				"personAddress(id: $addressId)": queryStruct2(),
				"personPoint(where: $where)":    queryStruct3(),
			},
			variables: map[string]interface{}{
				"id":        nil,
				"addressId": nil,
			},
			variableMismatched: true,
		},
	}

	for i, f := range fixtures {
		builder := graphql.Builder{}
		for q, b := range f.queries {
			builder = builder.Bind(q, b)
		}
		for k, v := range f.variables {
			builder = builder.Variable(k, v)
		}

		queries, vars, err := builder.Build()
		if f.variableMismatched && err != nil {
			if !strings.Contains(err.Error(), "mismatched variables;") {
				t.Errorf("[%d] got: %+v, want mismatched variables error", i, err)
			}
		} else if testEqualError(t, f.err, err, fmt.Sprintf("[%d]", i)) && f.err == nil && err == nil {
			testEqualMap(t, f.variables, vars, fmt.Sprintf("[%d]", i))
			queryFailed := len(queries) != len(f.queries)
			if !queryFailed {
				for _, q := range queries {
					wantQuery, ok := f.queries[q[0].(string)]
					if !ok || wantQuery != q[1] {
						queryFailed = true
						break
					}
				}
			}
			if queryFailed {
				t.Errorf("[%d] queries mismatched. got: %+v, want: %+v", i, queries, f.queries)
			}
		}
	}
}

func TestBuilder_remove(t *testing.T) {

	var queryStruct1 struct {
		ID   string
		Name string
	}

	var queryStruct2 struct {
		ID        string
		Email     string
		Addresses []struct {
			Street string
			City   string
		}
	}

	var queryStruct3 struct {
		ID     string
		Email  string
		Points []string
	}

	fixture := struct {
		queries   map[string]interface{}
		variables map[string]interface{}
	}{
		queries: map[string]interface{}{
			"person(id: $id)":               queryStruct1,
			"personAddress(id: $addressId)": queryStruct2,
			"personPoint(where: $where)":    queryStruct3,
		},
		variables: map[string]interface{}{
			"id":        nil,
			"addressId": nil,
			"where":     nil,
		},
	}

	builder := graphql.Builder{}
	for q, b := range fixture.queries {
		builder = builder.Bind(q, b)
	}
	for k, v := range fixture.variables {
		builder = builder.Variable(k, v)
	}

	builder = builder.RemoveQuery("person(id: $id)")
	_, _, e1 := builder.Build()

	if e1 == nil || !strings.Contains(e1.Error(), "mismatched variables;") {
		t.Errorf("remove query failed, got: nil, want mismatched error")
	}

	builder = builder.RemoveVariable("id")
	q2, v2, e2 := builder.Build()

	if e2 != nil {
		t.Errorf("remove query failed, got: %+v, want nil error", e2)
	}

	expected2 := [][2]interface{}{
		{"personAddress(id: $addressId)", queryStruct2},
		{"personPoint(where: $where)", queryStruct3},
	}
	if len(q2) != 2 {
		t.Errorf("remove query failed, got: %+v, want %+v", q2, expected2)
	}

	testEqualMap(t, v2, map[string]interface{}{
		"addressId": nil,
		"where":     nil,
	}, "remove query failed")

	builder = builder.Unbind("personPoint(where: $where)")
	q3, v3, e3 := builder.Build()

	if e3 != nil {
		t.Errorf("remove query failed, got: %+v, want nil error", e3)
	}

	if len(q3) != 1 || q3[0][0] != "personAddress(id: $addressId)" {
		t.Errorf("remove query failed, got: %+v, want %+v", q2, expected2)
	}

	testEqualMap(t, v3, map[string]interface{}{
		"addressId": nil,
	}, "remove query failed")

}

// Test Builder query output
func TestBuilder_Query(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		body := mustRead(req.Body)
		if got, want := body, `{"query":"query ($id:String!){user(id: $id){id,name}}","variables":{"id":"1"}}`+"\n"; got != want {
			t.Errorf("got body: %v, want %v", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		mustWrite(w, `{"data": {"user": {"id": "1", "name": "Gopher"}}}`)
	})
	client := graphql.NewClient("/graphql", &http.Client{Transport: localRoundTripper{handler: mux}})

	var user struct {
		ID   string `graphql:"id"`
		Name string `graphql:"name"`
	}

	err := graphql.NewBuilder().
		Bind("user(id: $id)", &user).
		Variable("id", "1").
		Query(client)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := user.Name, "Gopher"; got != want {
		t.Errorf("got user.Name: %q, want: %q", got, want)
	}
	if got, want := user.ID, "1"; got != want {
		t.Errorf("got user.ID: %q, want: %q", got, want)
	}
}

// Test Builder mutation output
func TestBuilder_Mutate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		body := mustRead(req.Body)
		if got, want := body, `{"query":"mutation ($id:String!){user(id: $id){id,name}}","variables":{"id":"1"}}`+"\n"; got != want {
			t.Errorf("got body: %v, want %v", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		mustWrite(w, `{"data": {"user": {"id": "1", "name": "Gopher"}}}`)
	})
	client := graphql.NewClient("/graphql", &http.Client{Transport: localRoundTripper{handler: mux}})

	var user struct {
		ID   string `graphql:"id"`
		Name string `graphql:"name"`
	}

	err := graphql.NewBuilder().
		Bind("user(id: $id)", &user).
		Variable("id", "1").
		Mutate(client)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := user.Name, "Gopher"; got != want {
		t.Errorf("got user.Name: %q, want: %q", got, want)
	}
	if got, want := user.ID, "1"; got != want {
		t.Errorf("got user.ID: %q, want: %q", got, want)
	}
}

// Test query Builder output with multiple queries
func TestBuilder_MultipleQueries(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		body := mustRead(req.Body)
		if got, want := body, `{"query":"{user{id,name}person{id,email,points}}"}`+"\n"; got != want {
			t.Errorf("got body: %v, want %v", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		mustWrite(w, `{"data": {"user": {"id": "1", "name": "Gopher"},"person": [{"id": "2", "email": "gopher@domain", "points": ["1", "2"]}]}}`)
	})
	client := graphql.NewClient("/graphql", &http.Client{Transport: localRoundTripper{handler: mux}})

	var user struct {
		ID   string `graphql:"id"`
		Name string `graphql:"name"`
	}

	q2 := make([]struct {
		ID     string
		Email  string
		Points []string
	}, 0)

	err := graphql.NewBuilder().Bind("user", &user).Bind("person", &q2).Query(client)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := user.Name, "Gopher"; got != want {
		t.Errorf("got user.Name: %q, want: %q", got, want)
	}
	if got, want := user.ID, "1"; got != want {
		t.Errorf("got user.ID: %q, want: %q", got, want)
	}
	if got, want := q2[0].ID, "2"; got != want {
		t.Errorf("got q2.ID: %q, want: %q", got, want)
	}
	if got, want := q2[0].Email, "gopher@domain"; got != want {
		t.Errorf("got q2.Email: %q, want: %q", got, want)
	}
	if got, want := q2[0].Points, "[1 2]"; fmt.Sprint(got) != want {
		t.Errorf("got q2.Points: %q, want: %q", got, want)
	}
}

func testEqualError(t *testing.T, want error, got error, msg string) bool {
	if (got == nil && want == nil) || (got != nil && want != nil && got.Error() == want.Error()) {
		return true
	}
	if msg != "" {
		msg = msg + " "
	}

	t.Errorf("%sgot: %+v, want: %+v", msg, got, want)
	return false
}

func testEqualMap(t *testing.T, want map[string]interface{}, got map[string]interface{}, msg string) {
	failed := len(want) != len(got)
	if !failed && len(want) > 0 {
		for key, val := range want {
			v, ok := got[key]
			if !ok || v != val {
				failed = true
				break
			}
		}
	}

	if failed {
		if msg != "" {
			msg = msg + " "
		}
		t.Errorf("%sgot: %+v, want: %+v", msg, got, want)
	}
}
