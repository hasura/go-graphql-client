package graphql

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"testing"
	"time"
)

func TestSubscription_LifeCycleEvents(t *testing.T) {
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
	defer server.Shutdown(ctx)
	defer cancel()

	subscriptionClient = subscriptionClient.
		WithExitWhenNoSubscription(false).
		WithTimeout(3 * time.Second).
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
			(*t).Fatalf("got error: %v, want: nil", err)
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
		t.Fatalf("expected OnDisonnected event, got none")
	}
}
