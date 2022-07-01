package graphql

import (
	"context"
	"encoding/json"
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

func subscription_setupClients() (*Client, *SubscriptionClient) {
	endpoint := "http://localhost:8080/graphql"

	client := NewClient(endpoint, &http.Client{Transport: http.DefaultTransport})

	subscriptionClient := NewSubscriptionClient(endpoint).
		WithConnectionParams(map[string]interface{}{
			"headers": map[string]string{
				"foo": "bar",
			},
		}).WithLog(log.Println).
		OnError(func(sc *SubscriptionClient, err error) error {
			panic(err)
		})

	return client, subscriptionClient
}

func subscription_setupServer() *http.Server {

	// init graphQL schema
	s, err := graphql.ParseSchema(schema, newResolver())
	if err != nil {
		panic(err)
	}

	// graphQL handler
	mux := http.NewServeMux()
	graphQLHandler := graphqlws.NewHandlerFunc(s, &relay.Handler{Schema: s})
	mux.HandleFunc("/graphql", graphQLHandler)
	server := &http.Server{Addr: ":8080", Handler: mux}

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
			subscribers[randomID()] = s
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

func TestSubscriptionLifeCycle(t *testing.T) {
	stop := make(chan bool)
	server := subscription_setupServer()
	client, subscriptionClient := subscription_setupClients()
	msg := randomID()
	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer server.Shutdown(ctx)
	defer cancel()

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

	_, err := subscriptionClient.Subscribe(sub, nil, func(data *json.RawMessage, e error) error {
		if e != nil {
			t.Fatalf("got error: %v, want: nil", e)
			return nil
		}

		log.Println("result", string(*data))
		e = json.Unmarshal(*data, &sub)
		if e != nil {
			t.Fatalf("got error: %v, want: nil", e)
			return nil
		}

		if sub.HelloSaid.Message != String(msg) {
			t.Fatalf("subscription message does not match. got: %s, want: %s", sub.HelloSaid.Message, msg)
		}

		subscriptionClient.Close()
		stop <- true
		return nil
	})

	if err != nil {
		t.Fatalf("got error: %v, want: nil", err)
	}

	go subscriptionClient.Run()
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
