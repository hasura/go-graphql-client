package graphql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

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
		sub1 := subscriptionClient.GetSubscription(subId1)
		if sub1 == nil {
			t.Errorf("subscription 1 not found: %s", subId1)
			return
		} else {
			if sub1.GetKey() != subId1 {
				t.Errorf("subscription key 1 not equal, got %s, want %s", subId1, sub1.GetKey())
				return
			}
			if sub1.GetID() != subId1 {
				t.Errorf("subscription id 1 not equal, got %s, want %s", subId1, sub1.GetID())
				return
			}
		}
		sub2 := subscriptionClient.GetSubscription(subId2)
		if sub2 == nil {
			t.Errorf("subscription 2 not found: %s", subId2)
			return
		} else {
			if sub2.GetKey() != subId2 {
				t.Errorf("subscription id 2 not equal, got %s, want %s", subId2, sub2.GetKey())
				return
			}

			if sub2.GetID() != subId2 {
				t.Errorf("subscription id 2 not equal, got %s, want %s", subId2, sub2.GetID())
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
		sub1 := subscriptionClient.GetSubscription(subId1)
		if sub1 == nil {
			t.Errorf("subscription 1 not found: %s", subId1)
		} else {
			if sub1.GetKey() != subId1 {
				t.Errorf("subscription key 1 not equal, got %s, want %s", subId1, sub1.GetKey())
			}
			if sub1.GetID() == subId1 {
				t.Errorf("subscription id 1 should equal, got %s, want %s", subId1, sub1.GetID())
			}
		}
		sub2 := subscriptionClient.GetSubscription(subId2)
		if sub2 == nil {
			t.Errorf("subscription 2 not found: %s", subId2)
		} else {
			if sub2.GetKey() != subId2 {
				t.Errorf("subscription id 2 not equal, got %s, want %s", subId2, sub2.GetKey())
			}

			if sub2.GetID() == subId2 {
				t.Errorf("subscription id 2 should equal, got %s, want %s", subId2, sub2.GetID())
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
