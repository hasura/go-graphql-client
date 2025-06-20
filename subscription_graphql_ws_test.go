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

type user_insert_input map[string]interface{}

func hasura_setupClients(protocol SubscriptionProtocolType) (*Client, *SubscriptionClient) {
	endpoint := fmt.Sprintf("%s/v1/graphql", hasuraTestHost)
	client := NewClient(endpoint, http.DefaultClient).
		WithRequestModifier(func(r *http.Request) {
			r.Header.Set("x-hasura-admin-secret", hasuraTestAdminSecret)
		})

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
	letter := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

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
			t.Fatalf(
				"subscription message does not match. got: %s, want: %s",
				sub.Users[0].Name,
				msg,
			)
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
	if err := waitHasuraService(120); err != nil {
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
			t.Fatalf(
				"subscription message does not match. got: %s, want: %s",
				sub.Users[0].Name,
				msg,
			)
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
			t.Fatalf(
				"subscription message does not match. got: %s, want: %s",
				sub.Users[0].Name,
				msg,
			)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}

	go func() {
		if err := subscriptionClient.Run(); err == nil || !subscriptionClient.IsUnauthorized(err) {
			t.Errorf("got error: %v, want: unauthorized", err)
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

func TestSubscription_WithRetryStatusCodes(t *testing.T) {
	stop := make(chan bool)
	msg := randomID()
	disconnectedCount := 0
	subscriptionClient := NewSubscriptionClient(fmt.Sprintf("%s/v1/graphql", hasuraTestHost)).
		WithProtocol(GraphQLWS).
		WithRetryStatusCodes("4400", "4403").
		WithConnectionParams(map[string]interface{}{
			"headers": map[string]string{
				"x-hasura-admin-secret": "test",
			},
		}).WithLog(log.Println).
		OnDisconnected(func() {
			disconnectedCount++
			if disconnectedCount > 5 {
				stop <- true
			}
		}).
		OnError(func(sc *SubscriptionClient, err error) error {
			t.Fatal("should not receive error")
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
			t.Fatalf(
				"subscription message does not match. got: %s, want: %s",
				sub.Users[0].Name,
				msg,
			)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}

	go func() {
		if err := subscriptionClient.Run(); err != nil && websocket.CloseStatus(err) == 4400 {
			t.Errorf("should not get error 4400, got error: %v, want: nil", err)
		}
	}()

	defer subscriptionClient.Close()

	// wait until the subscription client connects to the server
	if err := waitHasuraService(60); err != nil {
		t.Fatalf("failed to start hasura service: %s", err)
	}

	<-stop
}

func TestSubscription_closeThenRun(t *testing.T) {
	_, subscriptionClient := hasura_setupClients(GraphQLWS)

	fixtures := []struct {
		Query        interface{}
		Variables    map[string]interface{}
		Subscription *Subscription
	}{
		{
			Query: func() interface{} {
				var t struct {
					Users []struct {
						ID   int    `graphql:"id"`
						Name string `graphql:"name"`
					} `graphql:"user(order_by: { id: desc }, limit: 5)"`
				}

				return t
			}(),
			Variables: nil,
			Subscription: &Subscription{
				payload: GraphQLRequestPayload{
					Query: "subscription{helloSaid{id,msg}}",
				},
			},
		},
		{
			Query: func() interface{} {
				var t struct {
					Users []struct {
						ID int `graphql:"id"`
					} `graphql:"user(order_by: { id: desc }, limit: 5)"`
				}

				return t
			}(),
			Variables: nil,
			Subscription: &Subscription{
				payload: GraphQLRequestPayload{
					Query: "subscription{helloSaid{msg}}",
				},
			},
		},
	}

	subscriptionClient = subscriptionClient.
		WithExitWhenNoSubscription(false).
		WithTimeout(3 * time.Second).
		OnError(func(sc *SubscriptionClient, err error) error {
			t.Fatalf("got error: %v, want: nil", err)
			return err
		})

	bulkSubscribe := func() {
		for _, f := range fixtures {
			id, err := subscriptionClient.Subscribe(
				f.Query,
				f.Variables,
				func(data []byte, e error) error {
					if e != nil {
						t.Fatalf("got error: %v, want: nil", e)
						return nil
					}
					return nil
				},
			)
			if err != nil {
				t.Fatalf("got error: %v, want: nil", err)
			}
			log.Printf("subscribed: %s", id)
		}
	}

	bulkSubscribe()

	go func() {
		if err := subscriptionClient.Run(); err != nil {
			t.Errorf("got error: %v, want: nil", err)
		}
	}()

	time.Sleep(3 * time.Second)
	if err := subscriptionClient.Close(); err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}

	go func() {
		length := len(subscriptionClient.GetSubscriptions())
		if length != 2 {
			t.Errorf("unexpected subscription client. got: %d, want: 2", length)
			return
		}

		if subscriptionClient.getCurrentSession() != nil {
			t.Error("unexpected nil session")
		}
		if err := subscriptionClient.Run(); err != nil {
			t.Errorf("got error: %v, want: nil", err)
		}
	}()

	time.Sleep(3 * time.Second)
	length := len(subscriptionClient.GetSubscriptions())
	if length != 2 {
		t.Fatalf(
			"unexpected subscription client after restart. got: %d, want: 2, subscriptions: %+v",
			length,
			subscriptionClient.currentSession.subscriptions,
		)
	}
	if err := subscriptionClient.Close(); err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}
}

// waitForConnectionState waits for the subscription client to reach a specific connection state
func waitForConnectionState(t *testing.T, sc *SubscriptionClient, tickerDuration time.Duration, timeout time.Duration, checkFn func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(tickerDuration)
	defer ticker.Stop()

	for {
		<-ticker.C
		if checkFn() {
			return true
		}

		if time.Now().After(deadline) {
			return false
		}
	}
}

func TestRunWithContext_GracefulShutdown(t *testing.T) {
	subscriptionClient := NewSubscriptionClient(fmt.Sprintf("%s/v1/graphql", hasuraTestHost)).
		WithConnectionParams(map[string]interface{}{
			"headers": map[string]string{
				"x-hasura-admin-secret": hasuraTestAdminSecret,
			},
		}).
		WithProtocol(GraphQLWS).
		WithLog(log.Println)

	ctx, cancel := context.WithCancel(context.Background())

	// Simulate a subscription
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
		return nil
	})
	if err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}

	// Run the subscription client in a separate goroutine and collect error
	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = subscriptionClient.RunWithContext(ctx)
		close(done)
	}()

	// Wait for the client to establish connection
	if !waitForConnectionState(t, subscriptionClient, 100*time.Millisecond, 5*time.Second, func() bool {
		session := subscriptionClient.getCurrentSession()
		return session != nil && session.GetAcknowledge()
	}) {
		t.Fatal("timeout waiting for connection to be established")
	}

	// Cancel the parent context to trigger graceful shutdown
	cancel()

	// Wait for the client to shut down
	if !waitForConnectionState(t, subscriptionClient, 100*time.Millisecond, 5*time.Second, func() bool {
		return subscriptionClient.getCurrentSession() == nil
	}) {
		t.Fatal("timeout waiting for connection to be closed")
	}

	// Wait for the Run goroutine to finish
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for Run goroutine to finish")
	}
	if runErr != nil {
		t.Errorf("got error: %v, want: nil", err)
	}
}
