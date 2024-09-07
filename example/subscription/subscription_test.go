package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
	"github.com/graph-gophers/graphql-transport-ws/graphqlws"
	gql "github.com/hasura/go-graphql-client"
)

func subscription_setupClients(port int) (*gql.Client, *gql.SubscriptionClient) {
	endpoint := fmt.Sprintf("http://localhost:%d/graphql", port)

	client := gql.NewClient(endpoint, &http.Client{Transport: http.DefaultTransport})

	subscriptionClient := gql.NewSubscriptionClient(endpoint).
		WithConnectionParams(map[string]interface{}{
			"headers": map[string]string{
				"foo": "bar",
			},
		}).WithLog(log.Println)

	return client, subscriptionClient
}

func subscription_setupServer(port int) *http.Server {

	// init graphQL schema
	s, err := graphql.ParseSchema(schema, newResolver())
	if err != nil {
		panic(err)
	}

	// graphQL handler
	mux := http.NewServeMux()
	graphQLHandler := graphqlws.NewHandlerFunc(s, &relay.Handler{Schema: s})
	mux.HandleFunc("/graphql", graphQLHandler)
	server := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}

	return server
}

func TestTransportWS_basicTest(t *testing.T) {
	stop := make(chan bool)
	server := subscription_setupServer(8081)
	client, subscriptionClient := subscription_setupClients(8081)
	msg := randomID()
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

	subscriptionClient.
		OnError(func(sc *gql.SubscriptionClient, err error) error {
			return err
		})

	/*
		subscription {
			helloSaid {
				id
				msg
			}
		}
	*/
	var sub struct {
		HelloSaid struct {
			ID      gql.String
			Message gql.String `graphql:"msg" json:"msg"`
		} `graphql:"helloSaid" json:"helloSaid"`
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

		if sub.HelloSaid.Message != gql.String(msg) {
			t.Fatalf("subscription message does not match. got: %s, want: %s", sub.HelloSaid.Message, msg)
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
			ID  gql.String
			Msg gql.String
		} `graphql:"sayHello(msg: $msg)"`
	}
	variables := map[string]interface{}{
		"msg": gql.String(msg),
	}
	err = client.Mutate(context.Background(), &q, variables, gql.OperationName("SayHello"))
	if err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}

	<-stop
}

func TestTransportWS_exitWhenNoSubscription(t *testing.T) {
	server := subscription_setupServer(8085)
	client, subscriptionClient := subscription_setupClients(8085)
	msg := randomID()
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
		WithTimeout(3 * time.Second).
		OnError(func(sc *gql.SubscriptionClient, err error) error {
			t.Fatalf("got error: %v, want: nil", err)
			return err
		}).
		OnDisconnected(func() {
			log.Println("disconnected")
		})
	/*
		subscription {
			helloSaid {
				id
				msg
			}
		}
	*/
	var sub struct {
		HelloSaid struct {
			ID      gql.String
			Message gql.String `graphql:"msg" json:"msg"`
		} `graphql:"helloSaid" json:"helloSaid"`
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

		if sub.HelloSaid.Message != gql.String(msg) {
			t.Fatalf("subscription message does not match. got: %s, want: %s", sub.HelloSaid.Message, msg)
		}

		return nil
	})

	if err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}

	/*
		subscription {
			helloSaid {
				id
				msg
			}
		}
	*/
	var sub2 struct {
		HelloSaid struct {
			Message gql.String `graphql:"msg" json:"msg"`
		} `graphql:"helloSaid" json:"helloSaid"`
	}

	subId2, err := subscriptionClient.Subscribe(sub2, nil, func(data []byte, e error) error {
		if e != nil {
			t.Fatalf("got error: %v, want: nil", e)
			return nil
		}

		log.Println("result", string(data))
		e = json.Unmarshal(data, &sub2)
		if e != nil {
			t.Fatalf("got error: %v, want: nil", e)
			return nil
		}

		if sub2.HelloSaid.Message != gql.String(msg) {
			t.Fatalf("subscription message does not match. got: %s, want: %s", sub2.HelloSaid.Message, msg)
		}

		return nil
	})

	if err != nil {
		t.Fatalf("got error: %v, want: nil", err)
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
				ID  gql.String
				Msg gql.String
			} `graphql:"sayHello(msg: $msg)"`
		}
		variables := map[string]interface{}{
			"msg": gql.String(msg),
		}
		err = client.Mutate(context.Background(), &q, variables, gql.OperationName("SayHello"))
		if err != nil {
			t.Errorf("got error: %v, want: nil", err)
			return
		}

		time.Sleep(2 * time.Second)
		_ = subscriptionClient.Unsubscribe(subId1)
		_ = subscriptionClient.Unsubscribe(subId2)
	}()

	defer subscriptionClient.Close()

	if err := subscriptionClient.Run(); err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}
}

func TestTransportWS_onDisconnected(t *testing.T) {
	port := 8083
	server := subscription_setupServer(port)
	var wasConnected bool
	disconnected := make(chan bool)
	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	// init client
	_, subscriptionClient := subscription_setupClients(port)
	subscriptionClient = subscriptionClient.
		WithTimeout(5 * time.Second).
		OnError(func(sc *gql.SubscriptionClient, err error) error {
			panic(err)
		}).
		OnConnected(func() {
			log.Println("OnConnected")
			wasConnected = true
		}).
		OnDisconnected(func() {
			log.Println("OnDisconnected")
			disconnected <- true
		})

	/*
		subscription {
			helloSaid {
				id
				msg
			}
		}
	*/
	var sub struct {
		HelloSaid struct {
			ID      gql.String
			Message gql.String `graphql:"msg" json:"msg"`
		} `graphql:"helloSaid" json:"helloSaid"`
	}

	_, err := subscriptionClient.Subscribe(sub, nil, func(data []byte, e error) error {
		if e != nil {
			t.Fatalf("got error: %v, want: nil", e)
		}
		return nil
	})

	if err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}

	// run client
	go func() {
		_ = subscriptionClient.Run()
	}()
	defer subscriptionClient.Close()

	// wait until the subscription client connects to the server
	time.Sleep(2 * time.Second)
	if err := server.Close(); err != nil {
		panic(err)
	}

	<-disconnected

	if !wasConnected {
		t.Fatal("the OnConnected event must be triggered")
	}
}

func testSubscription_LifeCycleEvents(t *testing.T, syncMode bool) {

	server := subscription_setupServer(8082)
	client, subscriptionClient := subscription_setupClients(8082)
	msg := randomID()

	var lock sync.Mutex
	subscriptionResults := []gql.Subscription{}
	var wasConnected, wasDisconnected int32
	addResult := func(s gql.Subscription) int {
		lock.Lock()
		defer lock.Unlock()
		subscriptionResults = append(subscriptionResults, s)
		return len(subscriptionResults)
	}

	fixtures := []struct {
		Query           interface{}
		Variables       map[string]interface{}
		ExpectedID      string
		ExpectedPayload gql.GraphQLRequestPayload
	}{
		{
			Query: func() interface{} {
				var t struct {
					HelloSaid struct {
						ID      gql.String
						Message gql.String `graphql:"msg" json:"msg"`
					} `graphql:"helloSaid" json:"helloSaid"`
				}

				return t
			}(),
			Variables: nil,
			ExpectedPayload: gql.GraphQLRequestPayload{
				Query: "subscription{helloSaid{id,msg}}",
			},
		},
		{
			Query: func() interface{} {
				var t struct {
					HelloSaid struct {
						Message gql.String `graphql:"msg" json:"msg"`
					} `graphql:"helloSaid" json:"helloSaid"`
				}

				return t
			}(),
			Variables: nil,
			ExpectedPayload: gql.GraphQLRequestPayload{
				Query: "subscription{helloSaid{msg}}",
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
			log.Println("connected")
			atomic.StoreInt32(&wasConnected, 1)
		}).
		OnError(func(sc *gql.SubscriptionClient, err error) error {
			t.Fatalf("got error: %v, want: nil", err)
			return err
		}).
		OnDisconnected(func() {
			log.Println("disconnected")
			atomic.StoreInt32(&wasDisconnected, 1)
		}).
		OnSubscriptionComplete(func(s gql.Subscription) {
			log.Println("OnSubscriptionComplete: ", s)
			length := addResult(s)
			if length == len(fixtures) {
				log.Println("done, closing...")
				subscriptionClient.Close()
			}
		})

	for i, f := range fixtures {
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
		fixtures[i].ExpectedID = id
		log.Printf("subscribed: %s", id)
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
				ID  gql.String
				Msg gql.String
			} `graphql:"sayHello(msg: $msg)"`
		}
		variables := map[string]interface{}{
			"msg": gql.String(msg),
		}
		err := client.Mutate(context.Background(), &q, variables, gql.OperationName("SayHello"))
		if err != nil {
			t.Errorf("got error: %v, want: nil", err)
			return
		}

		time.Sleep(2 * time.Second)
		for _, f := range fixtures {
			if err := subscriptionClient.Unsubscribe(f.ExpectedID); err != nil {
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
		if s.GetID() != fixtures[i].ExpectedID {
			t.Fatalf("%d: subscription id not matched, got: %s, want: %s", i, s.GetPayload().Query, fixtures[i].ExpectedPayload.Query)
		}
		if s.GetPayload().Query != fixtures[i].ExpectedPayload.Query {
			t.Fatalf("%d: query output not matched, got: %s, want: %s", i, s.GetPayload().Query, fixtures[i].ExpectedPayload.Query)
		}
	}

	// workaround for race condition
	time.Sleep(time.Second)

	if atomic.LoadInt32(&wasConnected) != 1 {
		t.Fatalf("expected OnConnected event, got none")
	}
	if atomic.LoadInt32(&wasDisconnected) != 1 {
		t.Fatalf("expected OnDisconnected event, got none")
	}
}

func TestSubscription_LifeCycleEvents(t *testing.T) {
	testSubscription_LifeCycleEvents(t, false)
}

func TestSubscription_WithSyncMode(t *testing.T) {
	testSubscription_LifeCycleEvents(t, true)
}
