package graphql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const (
	hasuraTestHost        = "http://localhost:8080"
	hasuraTestAdminSecret = "hasura"
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

func hasura_setupClients(protocol SubscriptionProtocolType) (*Client, *SubscriptionClient) {
	endpoint := fmt.Sprintf("%s/v1/graphql", hasuraTestHost)
	client := NewClient(endpoint, &http.Client{Transport: headerRoundTripper{
		setHeaders: func(req *http.Request) {
			req.Header.Set("x-hasura-admin-secret", hasuraTestAdminSecret)
		},
		rt: http.DefaultTransport,
	}})

	subscriptionClient := NewSubscriptionClient(endpoint).
		WithProtocol(protocol).
		WithConnectionParams(map[string]interface{}{
			"headers": map[string]string{
				"x-hasura-admin-secret": hasuraTestAdminSecret,
			},
		}).WithLog(log.Println)

	return client, subscriptionClient
}

func waitService(endpoint string, timeoutSecs int) error {
	var err error
	var res *http.Response
	for i := 0; i < timeoutSecs; i++ {
		res, err = http.Get(endpoint)
		if err == nil && res.StatusCode == 200 {
			return nil
		}

		time.Sleep(time.Second)
	}

	if err != nil {
		return err
	}

	if res != nil {
		body, err := io.ReadAll(res.Body)
		if err != nil {
			return errors.New(res.Status)
		}
		return errors.New(string(body))
	}
	return errors.New("unknown error")
}

func randomID() string {
	var letter = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	b := make([]rune, 16)
	for i := range b {
		b[i] = letter[rand.Intn(len(letter))]
	}
	return string(b)
}

func waitHasuraService(timeoutSecs int) error {
	return waitService(fmt.Sprintf("%s/healthz", hasuraTestHost), timeoutSecs)
}

func TestGraphqlWS_Subscription(t *testing.T) {
	stop := make(chan bool)
	client, subscriptionClient := hasura_setupClients(GraphQLWS)
	msg := randomID()

	hasKeepAlive := false

	subscriptionClient = subscriptionClient.
		OnConnectionAlive(func() {
			hasKeepAlive = true
		}).
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
			t.Errorf("got error: %v, want: exit", err)
		}
		stop <- true
	}()

	defer subscriptionClient.Close()

	// wait until the subscription client connects to the server
	if err := waitHasuraService(60); err != nil {
		t.Fatalf("failed to start hasura service: %s", err)
	}

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

	if !hasKeepAlive {
		t.Fatalf("expected OnConnectionAlive event, got none")
	}
}

func TestGraphqlWS_SubscriptionRerun(t *testing.T) {
	client, subscriptionClient := hasura_setupClients(GraphQLWS)
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

	subId1, err := subscriptionClient.Subscribe(sub, nil, func(data []byte, e error) error {
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

		return nil
	})

	if err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}

	go func() {
		if err := subscriptionClient.Run(); err != nil {
			t.Errorf("got error: %v, want: nil", err)
		}
	}()

	defer subscriptionClient.Close()

	// wait until the subscription client connects to the server
	if err := waitHasuraService(60); err != nil {
		t.Fatalf("failed to start hasura service: %s", err)
	}

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

	time.Sleep(2 * time.Second)
	go func() {
		time.Sleep(2 * time.Second)
		_ = subscriptionClient.Unsubscribe(subId1)
	}()

	if err := subscriptionClient.Run(); err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}
}

func TestGraphQLWS_OnError(t *testing.T) {
	stop := make(chan bool)

	subscriptionClient := NewSubscriptionClient(fmt.Sprintf("%s/v1/graphql", hasuraTestHost)).
		WithProtocol(GraphQLWS).
		WithConnectionParams(map[string]interface{}{
			"headers": map[string]string{
				"x-hasura-admin-secret": "test",
			},
		}).WithLog(log.Println)

	msg := randomID()

	subscriptionClient = subscriptionClient.
		OnConnected(func() {
			log.Println("client connected")
		}).
		OnError(func(sc *SubscriptionClient, err error) error {
			log.Println("OnError: ", err)
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

		return nil
	})

	if err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}

	go func() {
		if err := subscriptionClient.Run(); err == nil || websocket.CloseStatus(err) != 4400 {
			t.Errorf("got error: %v, want: 4400", err)
		}
		stop <- true
	}()

	defer subscriptionClient.Close()

	// wait until the subscription client connects to the server
	if err := waitHasuraService(60); err != nil {
		t.Fatalf("failed to start hasura service: %s", err)
	}

	<-stop
}
