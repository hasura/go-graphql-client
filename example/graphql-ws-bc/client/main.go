// subscription is a test program currently being used for developing graphql package.
// It performs queries against a local test GraphQL server instance.
//
// It's not meant to be a clean or readable example. But it's functional.
// Better, actual examples will be created in the future.
package main

import (
	"flag"
	"log"
	"net/http"

	graphql "github.com/hasura/go-graphql-client"
)

func main() {
	protocol := graphql.GraphQLWS
	protocolArg := flag.String("protocol", "graphql-ws", "The protocol is used for the subscription")
	flag.Parse()

	if protocolArg != nil {
		switch *protocolArg {
		case "graphql-ws":
		case "":
		case "ws":
			protocol = graphql.SubscriptionsTransportWS
		default:
			panic("invalid protocol. Accept [ws, graphql-ws]")
		}
	}

	if err := startSubscription(protocol); err != nil {
		panic(err)
	}
}

const serverEndpoint = "http://localhost:4000"

func startSubscription(protocol graphql.SubscriptionProtocolType) error {
	log.Printf("start subscription with protocol: %s", protocol)
	client := graphql.NewSubscriptionClient(serverEndpoint).
		WithWebSocketOptions(graphql.WebsocketOptions{
			HTTPHeader: http.Header{
				"Authorization": []string{"Bearer random-secret"},
			},
		}).
		WithLog(log.Println).
		WithProtocol(protocol).
		WithoutLogTypes(graphql.GQLData, graphql.GQLConnectionKeepAlive).
		OnError(func(sc *graphql.SubscriptionClient, err error) error {
			log.Print("err", err)
			return err
		})

	defer client.Close()

	/*
		subscription {
			greetings
		}
	*/
	var sub struct {
		Greetings string `graphql:"greetings"`
	}

	_, err := client.Subscribe(sub, nil, func(data []byte, err error) error {

		if err != nil {
			log.Println(err)
			return nil
		}

		if data == nil {
			return nil
		}
		log.Printf("hello: %+v", string(data))
		return nil
	})

	if err != nil {
		panic(err)
	}

	return client.Run()
}
