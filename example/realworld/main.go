package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"net/http/httptest"
	"os"

	graphql "github.com/hasura/go-graphql-client"
)

func main() {
	flag.Parse()

	err := run()
	if err != nil {
		log.Println(err)
	}
}

func run() error {

	url := "https://rickandmortyapi.com/graphql"

	client := graphql.NewClient(url, nil)

	/*
		query {
				character(id: 1) {
			name
			}
		}
	*/
	var q struct {
		Character struct {
			Name graphql.String
		} `graphql:"character(id: $characterID)"`
	}
	variables := map[string]interface{}{
		"characterID": graphql.ID("1"),
	}

	err := client.Query(context.Background(), &q, variables)
	if err != nil {
		return err
	}
	print(q)

	return nil
}

// print pretty prints v to stdout. It panics on any error.
func print(v interface{}) {
	w := json.NewEncoder(os.Stdout)
	w.SetIndent("", "\t")
	err := w.Encode(v)
	if err != nil {
		panic(err)
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
