package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	graphql "github.com/hasura/go-graphql-client"
)

func getServerEndpoint() string {
	return fmt.Sprintf("http://localhost:%d/graphql", httpPort)
}

func startSubscription() error {

	client := graphql.NewSubscriptionClient(getServerEndpoint()).
		WithConnectionParams(map[string]interface{}{
			"headers": map[string]string{
				"foo": "bar",
			},
		}).WithLog(log.Println).
		WithoutLogTypes(graphql.GQL_DATA, graphql.GQL_CONNECTION_KEEP_ALIVE).
		OnError(func(sc *graphql.SubscriptionClient, err error) error {
			log.Print("err", err)
			return err
		})

	defer client.Close()

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
			ID      graphql.String
			Message graphql.String `graphql:"msg"`
		} `graphql:"helloSaid"`
	}

	_, err := client.Subscribe(sub, nil, func(data *json.RawMessage, err error) error {

		if err != nil {
			log.Println(err)
			return nil
		}

		if data == nil {
			return nil
		}
		log.Println(string(*data))
		return nil
	})

	if err != nil {
		panic(err)
	}

	return client.Run()
}

// send hello mutations to the graphql server, so the subscription client can receive messages
func startSendHello() {

	client := graphql.NewClient(getServerEndpoint(), &http.Client{Transport: http.DefaultTransport})

	for i := 0; i < 120; i++ {
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
				ID  graphql.String
				Msg graphql.String
			} `graphql:"sayHello(msg: $msg)"`
		}
		variables := map[string]interface{}{
			"msg": graphql.String(randomID()),
		}
		err := client.Mutate(context.Background(), &q, variables, graphql.OperationName("SayHello"))
		if err != nil {
			fmt.Println(err)
		}
		time.Sleep(time.Second)
	}
}
