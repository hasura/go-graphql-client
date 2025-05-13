package graphql

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Subscription transport follow Apollo's subscriptions-transport-ws protocol specification
// https://github.com/apollographql/subscriptions-transport-ws/blob/master/PROTOCOL.md

const (
	// The server may responses with this message to the GQL_CONNECTION_INIT from client, indicates the server rejected the connection.
	GQLConnectionError OperationMessageType = "connection_error"
	// Client sends this message to execute GraphQL operation.
	GQLStart OperationMessageType = "start"
	// Client sends this message in order to stop a running GraphQL operation execution (for example: unsubscribe).
	GQLStop OperationMessageType = "stop"
	// Client sends this message in order to stop a running GraphQL operation execution (for example: unsubscribe).
	GQLData OperationMessageType = "data"
	// Server message that should be sent right after each GQL_CONNECTION_ACK processed and then periodically to keep the client connection alive.
	// The client starts to consider the keep alive message only upon the first received keep alive message from the server.
	GQLConnectionKeepAlive OperationMessageType = "ka"
	// Client sends this message to terminate the connection.
	GQLConnectionTerminate OperationMessageType = "connection_terminate"

	// @deprecated: use GQLConnectionInit instead.
	GQL_CONNECTION_INIT = GQLConnectionInit
	// @deprecated: use GQLConnectionError instead.
	GQL_CONNECTION_ERROR = GQLConnectionError
	// @deprecated: use GQLStart instead.
	GQL_START = GQLStart
	// @deprecated: use GQLStop instead.
	GQL_STOP = GQLStop
	// @deprecated: use GQLError instead.
	GQL_ERROR = GQLError
	// @deprecated: use GQLData instead.
	GQL_DATA = GQLData
	// @deprecated: use GQLComplete instead.
	GQL_COMPLETE = GQLComplete
	// @deprecated: use GQLConnectionKeepAlive instead.
	GQL_CONNECTION_KEEP_ALIVE = GQLConnectionKeepAlive
	// @deprecated: use GQLConnectionAck instead.
	GQL_CONNECTION_ACK = GQLConnectionAck
	// @deprecated: use GQLConnectionTerminate instead.
	GQL_CONNECTION_TERMINATE = GQLConnectionTerminate
)

type subscriptionsTransportWS struct{}

// GetSubprotocols returns subprotocol names of the subscription transport.
func (stw subscriptionsTransportWS) GetSubprotocols() []string {
	return []string{"graphql-ws"}
}

// ConnectionInit sends a initial request to establish a connection within the existing socket.
func (stw *subscriptionsTransportWS) ConnectionInit(
	ctx *SubscriptionContext,
	connectionParams map[string]interface{},
) error {
	return connectionInit(ctx, connectionParams)
}

// Subscribe requests an graphql operation specified in the payload message.
func (stw *subscriptionsTransportWS) Subscribe(ctx *SubscriptionContext, sub Subscription) error {
	if sub.GetStatus() == SubscriptionRunning {
		return nil
	}

	payload, err := json.Marshal(sub.GetPayload())
	if err != nil {
		return err
	}

	// send start message to the server
	msg := OperationMessage{
		ID:      sub.id,
		Type:    GQLStart,
		Payload: payload,
	}

	if err := ctx.Send(msg, GQLStart); err != nil {
		return err
	}

	sub.SetStatus(SubscriptionRunning)
	ctx.SetSubscription(sub.GetKey(), &sub)

	return nil
}

// The input parameter is subscription ID that is returned from Subscribe function.
func (stw *subscriptionsTransportWS) Unsubscribe(ctx *SubscriptionContext, sub Subscription) error {
	// send stop message to the server
	msg := OperationMessage{
		ID:   sub.id,
		Type: GQLStop,
	}

	return ctx.Send(msg, GQLStop)
}

// OnMessage listens ongoing messages from server.
func (stw *subscriptionsTransportWS) OnMessage(
	ctx *SubscriptionContext,
	subscription Subscription,
	message OperationMessage,
) error {
	switch message.Type {
	case GQLError:
		ctx.Log(message, map[string]any{
			"source": "server",
		}, GQLError)
		var errs Errors

		jsonErr := json.Unmarshal(message.Payload, &errs)
		if jsonErr != nil {
			subscription.handler(nil, fmt.Errorf("%s", string(message.Payload)))

			return nil //nolint:nilerr
		}

		if len(errs) > 0 {
			subscription.handler(nil, errs)

			return nil
		}
	case GQLData:
		ctx.Log(message, map[string]any{
			"source": "server",
		}, GQLData)
		var out struct {
			Data   *json.RawMessage `json:"data"`
			Errors Errors           `json:"errors"`
		}

		if subscription.handler == nil {
			return nil
		}

		err := json.Unmarshal(message.Payload, &out)
		if err != nil {
			subscription.handler(nil, err)

			return nil
		}

		if len(out.Errors) > 0 {
			subscription.handler(nil, out.Errors)

			return nil
		}

		var outData []byte
		if out.Data != nil && len(*out.Data) > 0 {
			outData = *out.Data
		}

		subscription.handler(outData, nil)
	case GQLConnectionError, "conn_err":
		ctx.Log(message, map[string]any{
			"source": "server",
		}, GQLConnectionError)

		// try to parse the error object
		var payload any
		err := errors.New(string(message.Payload))

		jsonErr := json.Unmarshal(message.Payload, &payload)
		if jsonErr != nil {
			return err
		}

		var errMsg string

		if p, ok := payload.(map[string]any); ok {
			if msg, ok := p["error"]; ok {
				errMsg = fmt.Sprint(msg)
			} else if msg, ok := p["message"]; ok {
				errMsg = fmt.Sprint(msg)
			}

			return Error{
				Message:    errMsg,
				Extensions: p,
			}
		}

		if s, ok := payload.(string); ok {
			return errors.New(s)
		}

		return err
	case GQLComplete:
		ctx.Log(message, map[string]any{
			"source": "server",
		}, GQLComplete)
		sub := ctx.GetSubscription(message.ID)

		if sub == nil {
			ctx.OnSubscriptionComplete(Subscription{
				id: message.ID,
			})
		} else {
			ctx.OnSubscriptionComplete(*sub)
			ctx.SetSubscription(sub.GetKey(), nil)
		}
	case GQLConnectionKeepAlive:
		ctx.Log(message, map[string]any{
			"source": "server",
		}, GQLConnectionKeepAlive)
		ctx.OnConnectionAlive()
	case GQLConnectionAck:
		// Expected response to the ConnectionInit message from the client acknowledging a successful connection with the server.
		// The client is now ready to request subscription operations.
		ctx.Log(message, map[string]any{
			"source": "server",
		}, GQLConnectionAck)
		ctx.SetAcknowledge(true)
		subscriptions := ctx.GetSubscriptions()

		for id, sub := range subscriptions {
			if err := stw.Subscribe(ctx, sub); err != nil {
				ctx.Log(
					fmt.Sprintf(
						"failed to subscribe: %s; id: %s; query: %s",
						err,
						id,
						sub.payload.Query,
					),
					map[string]any{
						"source": "client",
					},
					GQLInternal,
				)

				return nil
			}
		}

		ctx.OnConnected()
	default:
		ctx.Log(message, map[string]any{
			"source": "server",
		}, GQLUnknown)
	}

	return nil
}

// Close terminates all subscriptions of the current websocket.
func (stw *subscriptionsTransportWS) Close(ctx *SubscriptionContext) error {
	// send terminate message to the server
	msg := OperationMessage{
		Type: GQLConnectionTerminate,
	}

	return ctx.Send(msg, GQLConnectionTerminate)
}
