package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
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
			Name string
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
