package graphql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
	"github.com/graph-gophers/graphql-transport-ws/graphqlws"
)

const schema = `
schema {
	subscription: Subscription
	mutation: Mutation
	query: Query
}
type Query {
	hello: String!
}
type Subscription {
	helloSaid(): HelloSaidEvent!
}
type Mutation {
	sayHello(msg: String!): HelloSaidEvent!
}
type HelloSaidEvent {
	id: String!
	msg: String!
}
`

func subscription_setupClients(port int) (*Client, *SubscriptionClient) {
	endpoint := fmt.Sprintf("http://localhost:%d/graphql", port)

	client := NewClient(endpoint, &http.Client{Transport: http.DefaultTransport})

	subscriptionClient := NewSubscriptionClient(endpoint).
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

type resolver struct {
	helloSaidEvents     chan *helloSaidEvent
	helloSaidSubscriber chan *helloSaidSubscriber
}

func newResolver() *resolver {
	r := &resolver{
		helloSaidEvents:     make(chan *helloSaidEvent),
		helloSaidSubscriber: make(chan *helloSaidSubscriber),
	}

	go r.broadcastHelloSaid()

	return r
}

func (r *resolver) Hello() string {
	return "Hello world!"
}

func (r *resolver) SayHello(args struct{ Msg string }) *helloSaidEvent {
	e := &helloSaidEvent{msg: args.Msg, id: randomID()}
	go func() {
		select {
		case r.helloSaidEvents <- e:
		case <-time.After(1 * time.Second):
		}
	}()
	return e
}

type helloSaidSubscriber struct {
	stop   <-chan struct{}
	events chan<- *helloSaidEvent
}

func (r *resolver) broadcastHelloSaid() {
	subscribers := map[string]*helloSaidSubscriber{}
	unsubscribe := make(chan string)

	// NOTE: subscribing and sending events are at odds.
	for {
		select {
		case id := <-unsubscribe:
			delete(subscribers, id)
		case s := <-r.helloSaidSubscriber:
			id := randomID()
			log.Println("new client subscribed: ", id)
			subscribers[id] = s
		case e := <-r.helloSaidEvents:
			for id, s := range subscribers {
				go func(id string, s *helloSaidSubscriber) {
					select {
					case <-s.stop:
						unsubscribe <- id
						return
					default:
					}

					select {
					case <-s.stop:
						unsubscribe <- id
					case s.events <- e:
					case <-time.After(time.Second):
					}
				}(id, s)
			}
		}
	}
}

func (r *resolver) HelloSaid(ctx context.Context) <-chan *helloSaidEvent {
	c := make(chan *helloSaidEvent)
	// NOTE: this could take a while
	r.helloSaidSubscriber <- &helloSaidSubscriber{events: c, stop: ctx.Done()}

	return c
}

type helloSaidEvent struct {
	id  string
	msg string
}

func (r *helloSaidEvent) Msg() string {
	return r.msg
}

func (r *helloSaidEvent) ID() string {
	return r.id
}

func randomID() string {
	var letter = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	b := make([]rune, 16)
	for i := range b {
		b[i] = letter[rand.Intn(len(letter))]
	}
	return string(b)
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
		OnError(func(sc *SubscriptionClient, err error) error {
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
			ID      String
			Message String `graphql:"msg" json:"msg"`
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

		if sub.HelloSaid.Message != String(msg) {
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
			ID  String
			Msg String
		} `graphql:"sayHello(msg: $msg)"`
	}
	variables := map[string]interface{}{
		"msg": String(msg),
	}
	err = client.Mutate(context.Background(), &q, variables, OperationName("SayHello"))
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
		OnError(func(sc *SubscriptionClient, err error) error {
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
			ID      String
			Message String `graphql:"msg" json:"msg"`
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

		if sub.HelloSaid.Message != String(msg) {
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
			Message String `graphql:"msg" json:"msg"`
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

		if sub2.HelloSaid.Message != String(msg) {
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
				ID  String
				Msg String
			} `graphql:"sayHello(msg: $msg)"`
		}
		variables := map[string]interface{}{
			"msg": String(msg),
		}
		err = client.Mutate(context.Background(), &q, variables, OperationName("SayHello"))
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

func TestTransportWS_ResetClient(t *testing.T) {

	stop := make(chan bool)
	client, subscriptionClient := hasura_setupClients(SubscriptionsTransportWS)
	msg := randomID()

	subscriptionClient.
		OnError(func(sc *SubscriptionClient, err error) error {
			t.Fatalf("got error: %v, want: nil", err)
			return err
		}).
		OnDisconnected(func() {
			log.Println("disconnected")
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

	defer subscriptionClient.Close()

	// wait until the subscription client connects to the server
	if err := waitHasuraService(60); err != nil {
		t.Fatalf("failed to start hasura service: %s", err)
	}

	/*
		subscription {
			user {
				id
				name
			}
		}
	*/
	var sub2 struct {
		Users []struct {
			ID int `graphql:"id"`
		} `graphql:"user(order_by: { id: desc }, limit: 5)"`
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

		if len(sub.Users) > 0 && sub.Users[0].Name != msg {
			t.Fatalf("subscription message does not match. got: %s, want: %s", sub.Users[0].Name, msg)
		}

		return nil
	})

	if err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}

	go func() {

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
			t.Errorf("got error: %v, want: nil", err)
			return
		}

		time.Sleep(2 * time.Second)

		// test subscription ids
		sub1 := subscriptionClient.getContext().GetSubscription(subId1)
		if sub1 == nil {
			t.Errorf("subscription 1 not found: %s", subId1)
			return
		} else {
			if sub1.key != subId1 {
				t.Errorf("subscription key 1 not equal, got %s, want %s", subId1, sub1.key)
				return
			}
			if sub1.id != subId1 {
				t.Errorf("subscription id 1 not equal, got %s, want %s", subId1, sub1.id)
				return
			}
		}
		sub2 := subscriptionClient.getContext().GetSubscription(subId2)
		if sub2 == nil {
			t.Errorf("subscription 2 not found: %s", subId2)
			return
		} else {
			if sub2.key != subId2 {
				t.Errorf("subscription id 2 not equal, got %s, want %s", subId2, sub2.key)
				return
			}

			if sub2.id != subId2 {
				t.Errorf("subscription id 2 not equal, got %s, want %s", subId2, sub2.id)
				return
			}
		}

		// reset the subscription
		log.Printf("resetting the subscription client...")
		if err := subscriptionClient.Run(); err != nil {
			t.Errorf("failed to reset the subscription client. got error: %v, want: nil", err)
		}
		log.Printf("the second run was stopped")
		stop <- true
	}()

	go func() {
		time.Sleep(8 * time.Second)

		// test subscription ids
		sub1 := subscriptionClient.getContext().GetSubscription(subId1)
		if sub1 == nil {
			t.Errorf("subscription 1 not found: %s", subId1)
		} else {
			if sub1.key != subId1 {
				t.Errorf("subscription key 1 not equal, got %s, want %s", subId1, sub1.key)
			}
			if sub1.id == subId1 {
				t.Errorf("subscription id 1 should equal, got %s, want %s", subId1, sub1.id)
			}
		}
		sub2 := subscriptionClient.getContext().GetSubscription(subId2)
		if sub2 == nil {
			t.Errorf("subscription 2 not found: %s", subId2)
		} else {
			if sub2.key != subId2 {
				t.Errorf("subscription id 2 not equal, got %s, want %s", subId2, sub2.key)
			}

			if sub2.id == subId2 {
				t.Errorf("subscription id 2 should equal, got %s, want %s", subId2, sub2.id)
			}
		}

		_ = subscriptionClient.Unsubscribe(subId1)
		_ = subscriptionClient.Unsubscribe(subId2)
	}()

	defer subscriptionClient.Close()

	if err := subscriptionClient.Run(); err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}

	<-stop
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
		OnError(func(sc *SubscriptionClient, err error) error {
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
			ID      String
			Message String `graphql:"msg" json:"msg"`
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

func TestTransportWS_OnError(t *testing.T) {
	stop := make(chan bool)

	subscriptionClient := NewSubscriptionClient(fmt.Sprintf("%s/v1/graphql", hasuraTestHost)).
		WithTimeout(3 * time.Second).
		WithProtocol(SubscriptionsTransportWS).
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
		unauthorizedErr := "invalid x-hasura-admin-secret/x-hasura-access-key"
		err := subscriptionClient.Run()

		if err == nil || err.Error() != unauthorizedErr {
			t.Errorf("got error: %v, want: %s", err, unauthorizedErr)
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
