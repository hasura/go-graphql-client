package main

import (
	"log"
	"net/http"
	"os"
	"time"

	graphql "github.com/hasura/go-graphql-client"
)

// https://developer.tibber.com/explorer

const (
	subscriptionEndpoint = "wss://websocket-api.tibber.com/v1-beta/gql/subscriptions"
)

func main() {
	if err := startSubscription(); err != nil {
		panic(err)
	}
}

// the subscription uses the Real time subscription demo
//
//	subscription LiveMeasurement($homeId: ID!) {
//		liveMeasurement(homeId: $homeId){
//			timestamp
//			power
//			accumulatedConsumption
//			accumulatedCost
//			currency
//			minPower
//			averagePower
//			maxPower
//		}
//	}
func startSubscription() error {
	// get the demo token from the graphiql playground
	demoToken := os.Getenv("TIBBER_DEMO_TOKEN")
	if demoToken == "" {
		panic("TIBBER_DEMO_TOKEN env variable is required")
	}

	client := graphql.NewSubscriptionClient(subscriptionEndpoint).
		WithProtocol(graphql.GraphQLWS).
		WithWebSocketOptions(graphql.WebsocketOptions{
			HTTPClient: &http.Client{
				Transport: headerRoundTripper{
					setHeaders: func(req *http.Request) {
						req.Header.Set("User-Agent", "go-graphql-client/0.9.0")
					},
					rt: http.DefaultTransport,
				},
			},
		}).
		WithConnectionParams(map[string]interface{}{
			"token": demoToken,
		}).WithLog(log.Println).
		OnError(func(sc *graphql.SubscriptionClient, err error) error {
			panic(err)
		})

	defer client.Close()

	var sub struct {
		LiveMeasurement struct {
			Timestamp              time.Time `graphql:"timestamp"`
			Power                  int       `graphql:"power"`
			AccumulatedConsumption float64   `graphql:"accumulatedConsumption"`
			AccumulatedCost        float64   `graphql:"accumulatedCost"`
			Currency               string    `graphql:"currency"`
			MinPower               int       `graphql:"minPower"`
			AveragePower           float64   `graphql:"averagePower"`
			MaxPower               float64   `graphql:"maxPower"`
		} `graphql:"liveMeasurement(homeId: $homeId)"`
	}

	variables := map[string]interface{}{
		"homeId": graphql.ID("96a14971-525a-4420-aae9-e5aedaa129ff"),
	}
	_, err := client.Subscribe(sub, variables, func(data []byte, err error) error {

		if err != nil {
			log.Println("ERROR: ", err)
			return nil
		}

		if data == nil {
			return nil
		}
		log.Println(string(data))
		return nil
	})

	if err != nil {
		panic(err)
	}

	return client.Run()
}

type headerRoundTripper struct {
	setHeaders func(req *http.Request)
	rt         http.RoundTripper
}

func (h headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	h.setHeaders(req)
	return h.rt.RoundTrip(req)
}
