package graphql

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"testing"
	"time"
)

type headerRoundTripper struct {
	setHeaders func(req *http.Request)
	rt         http.RoundTripper
}

func (h headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	h.setHeaders(req)
	return h.rt.RoundTrip(req)
}

type user_insert_input map[string]interface{}

func graphqlWS_setupClients() (*Client, *SubscriptionClient) {
	endpoint := "http://localhost:8080/v1/graphql"
	adminSecret := "hasura"
	client := NewClient(endpoint, &http.Client{Transport: headerRoundTripper{
		setHeaders: func(req *http.Request) {
			req.Header.Set("x-hasura-admin-secret", adminSecret)
		},
		rt: http.DefaultTransport,
	}})

	subscriptionClient := NewSubscriptionClient(endpoint).
		WithProtocol(GraphQLWS).
		WithConnectionParams(map[string]interface{}{
			"headers": map[string]string{
				"x-hasura-admin-secret": adminSecret,
			},
		}).WithLog(log.Println)

	return client, subscriptionClient
}

func TestGraphqlWS_Subscription(t *testing.T) {
	stop := make(chan bool)
	client, subscriptionClient := graphqlWS_setupClients()
	msg := randomID()

	subscriptionClient = subscriptionClient.
		OnError(func(sc *SubscriptionClient, err error) error {
			return err
		})

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
		} `graphql:"user(order_by: { id: desc }, limit: 5)"`
	}

	_, err := subscriptionClient.Subscribe(sub, nil, func(data []byte, e error) error {
		if e != nil {
			t.Fatalf("got error: %v, want: nil", e)
			return nil
		}

		log.Println("result", string(data))
		e = json.Unmarshal(data, &sub)
		if e != nil {
			t.Fatalf("got error: %v, want: nil", e)
			return nil
		}

		if len(sub.Users) > 0 && sub.Users[0].Name != msg {
			t.Fatalf("subscription message does not match. got: %s, want: %s", sub.Users[0].Name, msg)
		}

		return errors.New("exit")
	})

	if err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}

	go func() {
		if err := subscriptionClient.Run(); err == nil || err.Error() != "exit" {
			(*t).Fatalf("got error: %v, want: exit", err)
		}
		stop <- true
	}()

	defer subscriptionClient.Close()

	// wait until the subscription client connects to the server
	time.Sleep(2 * time.Second)

	// call a mutation request to send message to the subscription
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
				"name": msg,
			},
		},
	}
	err = client.Mutate(context.Background(), &q, variables, OperationName("InsertUser"))

	if err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}

	<-stop
}
