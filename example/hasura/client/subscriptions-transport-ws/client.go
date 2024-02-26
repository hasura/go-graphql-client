package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	graphql "github.com/hasura/go-graphql-client"
)

const (
	serverEndpoint     = "http://localhost:8080/v1/graphql"
	adminSecret        = "hasura"
	xHasuraAdminSecret = "x-hasura-admin-secret"
)

func main() {
	go insertUsers()
	if err := startSubscription(); err != nil {
		panic(err)
	}
}

func startSubscription() error {

	client := graphql.NewSubscriptionClient(serverEndpoint).
		WithConnectionParams(map[string]interface{}{
			"headers": map[string]string{
				xHasuraAdminSecret: adminSecret,
			},
		}).WithLog(log.Println).
		OnError(func(sc *graphql.SubscriptionClient, err error) error {
			log.Print("err", err)
			return err
		})

	defer client.Close()

	/*
		subscription {
			user {
				id
				name
			}
		}
	*/
	var sub struct {
		Users []struct {
			ID   int    `graphql:"id"`
			Name string `graphql:"name"`
		} `graphql:"user(limit: 5, order_by: { id: desc })"`
	}

	subId, err := client.Subscribe(sub, nil, func(data []byte, err error) error {

		if err != nil {
			log.Println(err)
			return nil
		}

		if data == nil {
			return nil
		}
		log.Println(string(data))
		return nil
	})

	if err != nil {
		panic(err)
	}

	// automatically unsubscribe after 10 seconds
	go func() {
		time.Sleep(10 * time.Second)
		_ = client.Unsubscribe(subId)
	}()

	return client.Run()
}

type user_insert_input map[string]interface{}

// insertUsers insert users to the graphql server, so the subscription client can receive messages
func insertUsers() {

	client := graphql.NewClient(serverEndpoint, &http.Client{
		Transport: headerRoundTripper{
			setHeaders: func(req *http.Request) {
				req.Header.Set(xHasuraAdminSecret, adminSecret)
			},
			rt: http.DefaultTransport,
		},
	})
	// stop until the subscription client is connected
	time.Sleep(time.Second)
	for i := 0; i < 10; i++ {
		/*
			mutation InsertUser($objects: [user_insert_input!]!) {
				insert_user(objects: $objects) {
					id
					name
				}
			}
		*/
		var q struct {
			InsertUser struct {
				Returning []struct {
					ID   int    `graphql:"id"`
					Name string `graphql:"name"`
				} `graphql:"returning"`
			} `graphql:"insert_user(objects: $objects)"`
		}
		variables := map[string]interface{}{
			"objects": []user_insert_input{
				{
					"name": randomString(),
				},
			},
		}
		err := client.Mutate(context.Background(), &q, variables, graphql.OperationName("InsertUser"))
		if err != nil {
			fmt.Println(err)
		}
		time.Sleep(time.Second)
	}
}

func randomString() string {
	var letter = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	b := make([]rune, 16)
	for i := range b {
		b[i] = letter[rand.Intn(len(letter))]
	}
	return string(b)
}

type headerRoundTripper struct {
	setHeaders func(req *http.Request)
	rt         http.RoundTripper
}

func (h headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	h.setHeaders(req)
	return h.rt.RoundTrip(req)
}
