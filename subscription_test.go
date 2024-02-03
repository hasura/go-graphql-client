package graphql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func testSubscription_LifeCycleEvents(t *testing.T, syncMode bool) {

	server := subscription_setupServer(8082)
	client, subscriptionClient := subscription_setupClients(8082)
	msg := randomID()

	var lock sync.Mutex
	subscriptionResults := []Subscription{}
	wasConnected := false
	wasDisconnected := false
	addResult := func(s Subscription) int {
		lock.Lock()
		defer lock.Unlock()
		subscriptionResults = append(subscriptionResults, s)
		return len(subscriptionResults)
	}

	fixtures := []struct {
		Query        interface{}
		Variables    map[string]interface{}
		Subscription *Subscription
	}{
		{
			Query: func() interface{} {
				var t struct {
					HelloSaid struct {
						ID      String
						Message String `graphql:"msg" json:"msg"`
					} `graphql:"helloSaid" json:"helloSaid"`
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
					HelloSaid struct {
						Message String `graphql:"msg" json:"msg"`
					} `graphql:"helloSaid" json:"helloSaid"`
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

	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer func() {
		_ = server.Shutdown(ctx)
	}()

	defer cancel()

	subscriptionClient = subscriptionClient.
		WithExitWhenNoSubscription(false).
		WithTimeout(3 * time.Second).
		WithSyncMode(syncMode).
		OnConnected(func() {
			lock.Lock()
			defer lock.Unlock()
			log.Println("connected")
			wasConnected = true
		}).
		OnError(func(sc *SubscriptionClient, err error) error {
			t.Fatalf("got error: %v, want: nil", err)
			return err
		}).
		OnDisconnected(func() {
			lock.Lock()
			defer lock.Unlock()
			log.Println("disconnected")
			wasDisconnected = true
		}).
		OnSubscriptionComplete(func(s Subscription) {
			log.Println("OnSubscriptionComplete: ", s)
			length := addResult(s)
			if length == len(fixtures) {
				log.Println("done, closing...")
				subscriptionClient.Close()
			}
		})

	for _, f := range fixtures {
		id, err := subscriptionClient.Subscribe(f.Query, f.Variables, func(data []byte, e error) error {
			lock.Lock()
			defer lock.Unlock()
			if e != nil {
				t.Fatalf("got error: %v, want: nil", e)
				return nil
			}

			log.Println("result", string(data))
			e = json.Unmarshal(data, &f.Query)
			if e != nil {
				t.Fatalf("got error: %v, want: nil", e)
				return nil
			}

			return nil
		})

		if err != nil {
			t.Fatalf("got error: %v, want: nil", err)
		}
		f.Subscription.id = id
		log.Printf("subscribed: %s; subscriptions %+v", id, subscriptionClient.context.subscriptions)
	}

	go func() {
		// wait until the subscription client connects to the server
		time.Sleep(2 * time.Second)

		// call a mutation request to send message to the subscription
		/*
			mutation ($msg: String!) {
				sayHello(msg: $msg) {
					id
					msg
				}
			}
		*/
		var q struct {
			SayHello struct {
				ID  String
				Msg String
			} `graphql:"sayHello(msg: $msg)"`
		}
		variables := map[string]interface{}{
			"msg": String(msg),
		}
		err := client.Mutate(context.Background(), &q, variables, OperationName("SayHello"))
		if err != nil {
			t.Errorf("got error: %v, want: nil", err)
			return
		}

		time.Sleep(2 * time.Second)
		for _, f := range fixtures {
			log.Println("unsubscribing ", f.Subscription.id)
			if err := subscriptionClient.Unsubscribe(f.Subscription.id); err != nil {
				log.Printf("subscriptions: %+v", subscriptionClient.context.subscriptions)
				panic(err)

			}
			time.Sleep(time.Second)
		}
	}()

	defer subscriptionClient.Close()

	if err := subscriptionClient.Run(); err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}

	if len(subscriptionResults) != len(fixtures) {
		t.Fatalf("failed to listen OnSubscriptionComplete event. got %+v, want: %+v", len(subscriptionResults), len(fixtures))
	}
	for i, s := range subscriptionResults {
		if s.id != fixtures[i].Subscription.id {
			t.Fatalf("%d: subscription id not matched, got: %s, want: %s", i, s.GetPayload().Query, fixtures[i].Subscription.payload.Query)
		}
		if s.GetPayload().Query != fixtures[i].Subscription.payload.Query {
			t.Fatalf("%d: query output not matched, got: %s, want: %s", i, s.GetPayload().Query, fixtures[i].Subscription.payload.Query)
		}
	}

	if !wasConnected {
		t.Fatalf("expected OnConnected event, got none")
	}
	if !wasDisconnected {
		t.Fatalf("expected OnDisconnected event, got none")
	}
}

func TestSubscription_LifeCycleEvents(t *testing.T) {
	testSubscription_LifeCycleEvents(t, false)
}

func TestSubscription_WithSyncMode(t *testing.T) {
	testSubscription_LifeCycleEvents(t, true)
}

func TestSubscription_WithRetryStatusCodes(t *testing.T) {
	stop := make(chan bool)
	msg := randomID()
	disconnectedCount := 0
	subscriptionClient := NewSubscriptionClient(fmt.Sprintf("%s/v1/graphql", hasuraTestHost)).
		WithProtocol(GraphQLWS).
		WithRetryStatusCodes("4400").
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
			t.Fatalf("subscription message does not match. got: %s, want: %s", sub.Users[0].Name, msg)
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

func TestSubscription_parseInt32Ranges(t *testing.T) {
	fixtures := []struct {
		Input    []string
		Expected [][]int32
		Error    error
	}{
		{
			Input:    []string{"1", "2", "3-5"},
			Expected: [][]int32{{1}, {2}, {3, 5}},
		},
		{
			Input: []string{"a", "2", "3-5"},
			Error: errors.New("invalid status code; input: a"),
		},
	}

	for i, f := range fixtures {
		output, err := parseInt32Ranges(f.Input)
		if f.Expected != nil && fmt.Sprintf("%v", output) != fmt.Sprintf("%v", f.Expected) {
			t.Fatalf("%d: got: %+v, want: %+v", i, output, f.Expected)
		}
		if f.Error != nil && f.Error.Error() != err.Error() {
			t.Fatalf("%d: error should equal, got: %+v, want: %+v", i, err, f.Error)
		}
	}
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
			id, err := subscriptionClient.Subscribe(f.Query, f.Variables, func(data []byte, e error) error {
				if e != nil {
					t.Fatalf("got error: %v, want: nil", e)
					return nil
				}
				return nil
			})

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

	bulkSubscribe()

	go func() {
		length := subscriptionClient.getContext().GetSubscriptionsLength(nil)
		if length != 2 {
			t.Errorf("unexpected subscription client. got: %d, want: 2", length)
			return
		}

		waitingLen := subscriptionClient.getContext().GetSubscriptionsLength([]SubscriptionStatus{SubscriptionWaiting})
		if waitingLen != 2 {
			t.Errorf("unexpected waiting subscription client. got: %d, want: 2", waitingLen)
		}
		if err := subscriptionClient.Run(); err != nil {
			t.Errorf("got error: %v, want: nil", err)
		}
	}()

	time.Sleep(3 * time.Second)
	length := subscriptionClient.getContext().GetSubscriptionsLength(nil)
	if length != 2 {
		t.Fatalf("unexpected subscription client after restart. got: %d, want: 2, subscriptions: %+v", length, subscriptionClient.context.subscriptions)
	}
	if err := subscriptionClient.Close(); err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}
}
