package graphql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
)

// SubscriptionProtocolType represents the protocol specification enum of the subscription
type SubscriptionProtocolType string

// internal subscription status
type SubscriptionStatus int32

const (
	// internal state machine status
	scStatusInitializing int32 = 0
	scStatusRunning      int32 = 1
	scStatusClosing      int32 = 2

	// SubscriptionWaiting the subscription hasn't been registered to the server
	SubscriptionWaiting SubscriptionStatus = 0
	// SubscriptionRunning the subscription is up and running
	SubscriptionRunning SubscriptionStatus = 1
	// SubscriptionUnsubscribed the subscription was manually unsubscribed by the user
	SubscriptionUnsubscribed SubscriptionStatus = 2

	// SubscriptionsTransportWS the enum implements the subscription transport that follows Apollo's subscriptions-transport-ws protocol specification
	// https://github.com/apollographql/subscriptions-transport-ws/blob/master/PROTOCOL.md
	SubscriptionsTransportWS SubscriptionProtocolType = "subscriptions-transport-ws"

	// GraphQLWS enum implements GraphQL over WebSocket Protocol (graphql-ws)
	// https://github.com/enisdenjo/graphql-ws/blob/master/PROTOCOL.md
	GraphQLWS SubscriptionProtocolType = "graphql-ws"

	// Receiving a message of a type or format which is not specified in this document
	// The <error-message> can be vaguely descriptive on why the received message is invalid.
	StatusInvalidMessage websocket.StatusCode = 4400
	// if the connection is not acknowledged, the socket will be closed immediately with the event 4401: Unauthorized
	StatusUnauthorized websocket.StatusCode = 4401
	// Connection initialisation timeout
	StatusConnectionInitialisationTimeout websocket.StatusCode = 4408
	// Subscriber for <generated-id> already exists
	StatusSubscriberAlreadyExists websocket.StatusCode = 4409
	// Too many initialisation requests
	StatusTooManyInitialisationRequests websocket.StatusCode = 4429
)

// OperationMessageType represents a subscription message enum type
type OperationMessageType string

const (
	// Unknown operation type, for logging only
	GQLUnknown OperationMessageType = "unknown"
	// Internal status, for logging only
	GQLInternal OperationMessageType = "internal"

	// @deprecated: use GQLUnknown instead
	GQL_UNKNOWN = GQLUnknown
	// @deprecated: use GQLInternal instead
	GQL_INTERNAL = GQLInternal
)

var (
	// ErrSubscriptionStopped a special error which forces the subscription stop
	ErrSubscriptionStopped = errors.New("subscription stopped")
	// ErrSubscriptionNotExists an error denoting that subscription does not exist
	ErrSubscriptionNotExists = errors.New("subscription does not exist")

	errRetry = errors.New("retry subscription client")
)

// OperationMessage represents a subscription operation message
type OperationMessage struct {
	ID      string               `json:"id,omitempty"`
	Type    OperationMessageType `json:"type"`
	Payload json.RawMessage      `json:"payload,omitempty"`
}

// String overrides the default Stringer to return json string for debugging
func (om OperationMessage) String() string {
	bs, _ := json.Marshal(om)

	return string(bs)
}

// WebsocketHandler abstracts WebSocket connection functions
// ReadJSON and WriteJSON data of a frame from the WebSocket connection.
// Close the WebSocket connection.
type WebsocketConn interface {
	ReadJSON(v interface{}) error
	WriteJSON(v interface{}) error
	Ping() error
	Close() error
	// SetReadLimit sets the maximum size in bytes for a message read from the peer. If a
	// message exceeds the limit, the connection sends a close message to the peer
	// and returns ErrReadLimit to the application.
	SetReadLimit(limit int64)
	// GetCloseStatus tries to get WebSocket close status from error
	// return -1 if the error is unknown
	// https://www.iana.org/assignments/websocket/websocket.xhtml
	GetCloseStatus(error) int32
}

// SubscriptionProtocol abstracts the life-cycle of subscription protocol implementation for a specific transport protocol
type SubscriptionProtocol interface {
	// GetSubprotocols returns subprotocol names of the subscription transport
	// The graphql server depends on the Sec-WebSocket-Protocol header to return the correct message specification
	GetSubprotocols() []string
	// ConnectionInit sends a initial request to establish a connection within the existing socket
	ConnectionInit(ctx *SubscriptionContext, connectionParams map[string]interface{}) error
	// Subscribe requests an graphql operation specified in the payload message
	Subscribe(ctx *SubscriptionContext, sub Subscription) error
	// Unsubscribe sends a request to stop listening and complete the subscription
	Unsubscribe(ctx *SubscriptionContext, sub Subscription) error
	// OnMessage listens ongoing messages from server
	OnMessage(ctx *SubscriptionContext, subscription Subscription, message OperationMessage) error
	// Close terminates all subscriptions of the current websocket
	Close(ctx *SubscriptionContext) error
}

// SubscriptionContext represents a shared context for protocol implementations with the websocket connection inside
type SubscriptionContext struct {
	context.Context
	client        *SubscriptionClient
	websocketConn WebsocketConn

	connectionInitAt time.Time
	acknowledged     int32
	cancel           context.CancelFunc
	subscriptions    map[string]Subscription
	mutex            sync.Mutex
}

// Log prints condition logging with message type filters
func (sc *SubscriptionContext) Log(message interface{}, source string, opType OperationMessageType) {
	if sc.client.log == nil {
		return
	}

	for _, ty := range sc.client.disabledLogTypes {
		if ty == opType {
			return
		}
	}

	sc.client.log(message, source)
}

// OnConnectionAlive executes the OnConnectionAlive callback if exists.
func (sc *SubscriptionContext) OnConnectionAlive() {
	if sc.client != nil && sc.client.onConnectionAlive != nil {
		sc.client.onConnectionAlive()
	}
}

// OnConnected executes the OnConnected callback if exists.
func (sc *SubscriptionContext) OnConnected() {
	if sc.client != nil && sc.client.onConnected != nil {
		sc.client.onConnected()
	}
}

// OnDisconnected executes the OnDisconnected callback if exists.
func (sc *SubscriptionContext) OnDisconnected() {
	if sc.client != nil && sc.client.onDisconnected != nil {
		sc.client.onDisconnected()
	}
}

// OnSubscriptionComplete executes the OnSubscriptionComplete callback if exists.
func (sc *SubscriptionContext) OnSubscriptionComplete(subscription Subscription) {
	if sc.client != nil && sc.client.onSubscriptionComplete != nil {
		sc.client.onSubscriptionComplete(subscription)
	}
}

// GetContext get the inner context
func (sc *SubscriptionContext) GetContext() context.Context {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	return sc.Context
}

// GetContext set the inner context
func (sc *SubscriptionContext) NewContext() {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	sc.Context = ctx
	sc.cancel = cancel
}

// SetCancel set the cancel function of the inner context
func (sc *SubscriptionContext) Cancel() {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	if sc.cancel != nil {
		sc.cancel()
		sc.cancel = nil
	}
}

// GetWebsocketConn get the current websocket connection
func (sc *SubscriptionContext) GetWebsocketConn() WebsocketConn {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	return sc.websocketConn
}

// SetWebsocketConn set the current websocket connection
func (sc *SubscriptionContext) SetWebsocketConn(conn WebsocketConn) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	sc.websocketConn = conn
}

// GetSubscription get the subscription state by id
func (sc *SubscriptionContext) GetSubscription(id string) *Subscription {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	if sc.subscriptions == nil {
		return nil
	}

	sub, found := sc.subscriptions[id]
	if found {
		return &sub
	}

	for _, s := range sc.subscriptions {
		if id == s.id {
			return &s
		}
	}
	return nil
}

// GetSubscriptionsLength returns the length of subscriptions by status
func (sc *SubscriptionContext) GetSubscriptionsLength(status []SubscriptionStatus) int {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	if len(status) == 0 {
		return len(sc.subscriptions)
	}
	count := 0
	for _, sub := range sc.subscriptions {
		for _, s := range status {
			if sub.status == s {
				count++
				break
			}
		}
	}
	return count
}

// GetSubscription get all available subscriptions in the context
func (sc *SubscriptionContext) GetSubscriptions() map[string]Subscription {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	newMap := make(map[string]Subscription)
	for k, v := range sc.subscriptions {
		newMap[k] = v
	}

	return newMap
}

// SetSubscription set the input subscription state into the context
// if subscription is nil, removes the subscription from the map
func (sc *SubscriptionContext) SetSubscription(key string, sub *Subscription) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	if sub == nil {
		delete(sc.subscriptions, key)
	} else {
		sc.subscriptions[key] = *sub
	}
}

// GetAcknowledge get the acknowledge status
func (sc *SubscriptionContext) GetAcknowledge() bool {
	return atomic.LoadInt32(&sc.acknowledged) > 0
}

// SetAcknowledge set the acknowledge status
func (sc *SubscriptionContext) SetAcknowledge(value bool) {
	if value {
		atomic.StoreInt32(&sc.acknowledged, 1)
	} else {
		atomic.StoreInt32(&sc.acknowledged, 0)
	}
}

// Close closes the context and the inner websocket connection if exists
func (sc *SubscriptionContext) Close() error {
	var err error
	if conn := sc.GetWebsocketConn(); conn != nil {
		sc.SetWebsocketConn(nil)
		if sc.client.onDisconnected != nil {
			sc.client.onDisconnected()
		}
		err = conn.Close()
	}

	sc.Cancel()

	if errors.Is(err, net.ErrClosed) {
		return nil
	}

	return err
}

// Send emits a message to the graphql server
func (sc *SubscriptionContext) Send(message interface{}, opType OperationMessageType) error {
	if conn := sc.GetWebsocketConn(); conn != nil {
		sc.Log(message, "client", opType)

		return conn.WriteJSON(message)
	}

	return nil
}

// initializes the websocket connection
func (sc *SubscriptionContext) init(parentContext context.Context) error {
	now := time.Now()

	for {
		ctx, cancel := context.WithCancel(parentContext)
		conn, err := sc.client.createConn(ctx, sc.client.url, sc.client.websocketOptions)
		if err == nil {
			conn.SetReadLimit(sc.client.readLimit)
			// send connection init event to the server
			connectionParams := sc.client.connectionParams
			if sc.client.connectionParamsFn != nil {
				connectionParams = sc.client.connectionParamsFn()
			}

			sc.mutex.Lock()
			sc.websocketConn = conn
			sc.connectionInitAt = time.Now()
			sc.mutex.Unlock()

			err = sc.client.protocol.ConnectionInit(sc, connectionParams)
			if err == nil {
				sc.Context = ctx
				sc.cancel = cancel

				return nil
			} else {
				_ = conn.Close()
				sc.SetWebsocketConn(nil)
			}
		}

		cancel()

		if sc.client.retryTimeout > 0 && now.Add(sc.client.retryTimeout).Before(time.Now()) {
			sc.OnDisconnected()

			return err
		}

		sc.Log(fmt.Sprintf("%s. retry in %d second...", err.Error(), sc.client.retryDelay/time.Second), "client", GQLInternal)
		time.Sleep(sc.client.retryDelay)
	}
}

func (sc *SubscriptionContext) run() {
	for {
		select {
		case <-sc.Done():
			return
		default:
			var message OperationMessage
			conn := sc.websocketConn
			if conn == nil {
				return
			}

			if err := conn.ReadJSON(&message); err != nil {
				// manual EOF check
				if err == io.EOF || strings.Contains(err.Error(), "EOF") || errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "connection reset by peer") {
					sc.client.errorChan <- errRetry

					return
				}

				if errors.Is(err, context.Canceled) {
					return
				}

				closeStatus := conn.GetCloseStatus(err)

				for _, retryCode := range sc.client.retryStatusCodes {
					if (len(retryCode) == 1 && retryCode[0] == closeStatus) ||
						(len(retryCode) >= 2 && retryCode[0] <= closeStatus && closeStatus <= retryCode[1]) {
						sc.client.errorChan <- errRetry

						return
					}
				}

				switch websocket.StatusCode(closeStatus) {
				case websocket.StatusBadGateway, websocket.StatusNoStatusRcvd:
					sc.client.errorChan <- errRetry
					return
				case websocket.StatusNormalClosure, websocket.StatusAbnormalClosure:
					// close event from websocket client, exiting...
					sc.Cancel()
					return
				case websocket.StatusInternalError, StatusInvalidMessage, StatusConnectionInitialisationTimeout, StatusTooManyInitialisationRequests, StatusSubscriberAlreadyExists, StatusUnauthorized:
					sc.Log(err, "server", GQL_CONNECTION_ERROR)
					sc.client.errorChan <- err

					return
				}

				if sc.client.onError != nil {
					if err = sc.client.onError(sc.client, err); err != nil {
						// end the subscription if the callback return error
						sc.Cancel()

						return
					}
				}

				continue
			}

			sub := sc.GetSubscription(message.ID)
			if sub == nil {
				sub = &Subscription{}
			}

			execMessage := func() {
				if err := sc.client.protocol.OnMessage(sc, *sub, message); err != nil {
					sc.client.errorChan <- err
				}

				sc.client.checkSubscriptionStatuses(sc)
			}

			if sc.client.syncMode {
				execMessage()
			} else {
				go execMessage()
			}
		}
	}
}

// Keep alive subroutine to send ping on specified interval.
// Note that this is the keep-alive implementation of the Websocket protocol, not subscription.
func (sc *SubscriptionContext) startWebsocketKeepAlive(c WebsocketConn, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Ping the websocket. You might want to handle any potential errors.
			err := c.Ping()
			if err != nil {
				sc.Log("Failed to ping server", "client", GQLInternal)
				sc.client.errorChan <- errRetry

				return
			}
		case <-sc.Done():
			// If the context is cancelled, stop the pinging.
			return
		}
	}
}

// CreateWebSocketConnFunc represents the function interface to create a WebSocket connection.
type CreateWebSocketConnFunc func(ctx context.Context, endpoint string, options WebsocketOptions) (WebsocketConn, error)
type handlerFunc func(data []byte, err error) error

// Subscription stores the subscription declaration and its state
type Subscription struct {
	id      string
	key     string
	payload GraphQLRequestPayload
	handler func(data []byte, err error)
	status  SubscriptionStatus
}

// GetID returns the subscription ID
func (s Subscription) GetID() string {
	return s.id
}

// GetKey returns the unique key of the subscription map
// Key is the immutable id of the subscription that is generated the first time
// It is used for searching because the subscription id is refreshed whenever the client reset
func (s Subscription) GetKey() string {
	return s.key
}

// GetPayload returns the graphql request payload
func (s Subscription) GetPayload() GraphQLRequestPayload {
	return s.payload
}

// GetHandler a public getter for the subscription handler
func (s Subscription) GetHandler() func(data []byte, err error) {
	return s.handler
}

// GetStatus a public getter for the subscription status
func (s Subscription) GetStatus() SubscriptionStatus {
	return s.status
}

// SetStatus a public getter for the subscription status
func (s *Subscription) SetStatus(status SubscriptionStatus) {
	s.status = status
}

// Clone copies the current subscription with a new state.
// The ID is newly generated to avoid subscription id conflict errors from the server
func (s Subscription) Clone() Subscription {
	return Subscription{
		id:      uuid.NewString(),
		key:     s.key,
		status:  SubscriptionWaiting,
		payload: s.payload,
		handler: s.handler,
	}
}

// SubscriptionClient is a GraphQL subscription client.
type SubscriptionClient struct {
	url                string
	currentSession     *SubscriptionContext
	connectionParams   map[string]interface{}
	connectionParamsFn func() map[string]interface{}
	protocol           SubscriptionProtocol
	websocketOptions   WebsocketOptions
	clientStatus       int32
	createConn         CreateWebSocketConnFunc

	readLimit                       int64 // max size of response message. Default 10 MB
	retryTimeout                    time.Duration
	connectionInitialisationTimeout time.Duration

	exitWhenNoSubscription     bool
	syncMode                   bool
	websocketKeepAliveInterval time.Duration
	retryDelay                 time.Duration
	disabledLogTypes           []OperationMessageType
	log                        func(args ...interface{})
	retryStatusCodes           [][]int32
	rawSubscriptions           map[string]Subscription

	// user-defined callback events
	onConnected            func()
	onConnectionAlive      func()
	onDisconnected         func()
	onError                func(sc *SubscriptionClient, err error) error
	onSubscriptionComplete func(sub Subscription)

	errorChan chan error
	cancel    context.CancelFunc
	mutex     sync.Mutex
}

// NewSubscriptionClient constructs new subscription client
func NewSubscriptionClient(url string) *SubscriptionClient {
	protocol := &subscriptionsTransportWS{}

	return &SubscriptionClient{
		url:                        url,
		readLimit:                  10 * 1024 * 1024, // set default limit 10MB
		createConn:                 newWebsocketConn,
		retryTimeout:               time.Minute,
		errorChan:                  make(chan error),
		protocol:                   protocol,
		exitWhenNoSubscription:     true,
		websocketKeepAliveInterval: 0,
		retryDelay:                 1 * time.Second,
		rawSubscriptions:           make(map[string]Subscription),
		websocketOptions: WebsocketOptions{
			Subprotocols: protocol.GetSubprotocols(),
			ReadTimeout:  time.Minute,
			WriteTimeout: time.Minute,
		},
	}
}

// GetURL returns GraphQL server's URL
func (sc *SubscriptionClient) GetURL() string {
	return sc.url
}

// GetTimeout returns write timeout of websocket client.
// Deprecated: use GetWriteTimeout instead.
func (sc *SubscriptionClient) GetTimeout() time.Duration {
	return sc.websocketOptions.WriteTimeout
}

// GetWriteTimeout returns write timeout of websocket client.
func (sc *SubscriptionClient) GetWriteTimeout() time.Duration {
	return sc.websocketOptions.WriteTimeout
}

// GetReadTimeout returns read timeout of websocket client.
func (sc *SubscriptionClient) GetReadTimeout() time.Duration {
	return sc.websocketOptions.ReadTimeout
}

// GetContext returns current context of subscription client.
func (sc *SubscriptionClient) GetContext() context.Context {
	currentSession := sc.getCurrentSession()
	if currentSession == nil {
		return context.Background()
	}

	return currentSession
}

// GetSubscriptions get the list of active subscriptions
func (sc *SubscriptionClient) GetSubscriptions() map[string]Subscription {
	session := sc.getCurrentSession()
	if session != nil {
		return sc.getCurrentSession().GetSubscriptions()
	}

	newMap := make(map[string]Subscription)
	for k, v := range sc.rawSubscriptions {
		newMap[k] = v
	}

	return newMap
}

// GetSubscription get the subscription state by id
func (sc *SubscriptionClient) GetSubscription(id string) *Subscription {
	session := sc.getCurrentSession()
	if session != nil {
		return sc.getCurrentSession().GetSubscription(id)
	}

	sub, ok := sc.rawSubscriptions[id]
	if !ok {
		return nil
	}

	return &sub
}

// WithWebSocket replaces customized websocket client constructor
// In default, subscription client uses https://github.com/coder/websocket
func (sc *SubscriptionClient) WithWebSocket(fn CreateWebSocketConnFunc) *SubscriptionClient {
	sc.createConn = fn

	return sc
}

// WithProtocol changes the subscription protocol implementation by type.
// By default the subscription client uses the subscriptions-transport-ws protocol
func (sc *SubscriptionClient) WithProtocol(protocol SubscriptionProtocolType) *SubscriptionClient {
	switch protocol {
	case GraphQLWS:
		sc.protocol = &graphqlWS{}
	case SubscriptionsTransportWS:
		sc.protocol = &subscriptionsTransportWS{}
	default:
		panic(fmt.Sprintf("unknown subscription protocol %s", protocol))
	}

	sc.websocketOptions.Subprotocols = sc.protocol.GetSubprotocols()

	return sc
}

// WithCustomProtocol changes the subscription protocol that implements the SubscriptionProtocol interface.
func (sc *SubscriptionClient) WithCustomProtocol(protocol SubscriptionProtocol) *SubscriptionClient {
	sc.protocol = protocol
	sc.websocketOptions.Subprotocols = sc.protocol.GetSubprotocols()

	return sc
}

// WithWebSocketOptions provides options to the websocket client
func (sc *SubscriptionClient) WithWebSocketOptions(options WebsocketOptions) *SubscriptionClient {
	if len(options.Subprotocols) == 0 {
		options.Subprotocols = sc.websocketOptions.Subprotocols
	}

	if options.ReadTimeout == 0 {
		options.ReadTimeout = sc.websocketOptions.ReadTimeout
	}

	if options.WriteTimeout == 0 {
		options.WriteTimeout = sc.websocketOptions.WriteTimeout
	}

	sc.websocketOptions = options

	return sc
}

// WithConnectionParams updates connection params for sending to server through GQL_CONNECTION_INIT event
// It's usually used for authentication handshake
func (sc *SubscriptionClient) WithConnectionParams(params map[string]interface{}) *SubscriptionClient {
	sc.connectionParams = params

	return sc
}

// WithConnectionParamsFn set a function that returns connection params for sending to server through GQL_CONNECTION_INIT event
// It's suitable for short-lived access tokens that need to be refreshed frequently
func (sc *SubscriptionClient) WithConnectionParamsFn(fn func() map[string]interface{}) *SubscriptionClient {
	sc.connectionParamsFn = fn

	return sc
}

// WithTimeout updates read and write timeout of websocket client.
func (sc *SubscriptionClient) WithTimeout(timeout time.Duration) *SubscriptionClient {
	sc.websocketOptions.WriteTimeout = timeout
	sc.websocketOptions.ReadTimeout = timeout

	return sc
}

// WithReadTimeout updates read timeout of websocket client.
func (sc *SubscriptionClient) WithReadTimeout(timeout time.Duration) *SubscriptionClient {
	sc.websocketOptions.ReadTimeout = timeout

	return sc
}

// WithWriteTimeout updates write timeout of websocket client.
func (sc *SubscriptionClient) WithWriteTimeout(timeout time.Duration) *SubscriptionClient {
	sc.websocketOptions.WriteTimeout = timeout

	return sc
}

// WithConnectionInitialisationTimeout updates timeout for the connection initialisation.
func (sc *SubscriptionClient) WithConnectionInitialisationTimeout(timeout time.Duration) *SubscriptionClient {
	sc.connectionInitialisationTimeout = timeout

	return sc
}

// WithRetryTimeout updates reconnecting timeout. When the websocket server was stopped, the client will retry connecting every second until timeout
// The zero value means unlimited timeout
func (sc *SubscriptionClient) WithRetryTimeout(timeout time.Duration) *SubscriptionClient {
	sc.retryTimeout = timeout
	return sc
}

// WithExitWhenNoSubscription the client should exit when all subscriptions were closed
func (sc *SubscriptionClient) WithExitWhenNoSubscription(value bool) *SubscriptionClient {
	sc.exitWhenNoSubscription = value
	return sc
}

// WithSyncMode subscription messages are executed in sequence (without goroutine)
func (sc *SubscriptionClient) WithSyncMode(value bool) *SubscriptionClient {
	sc.syncMode = value
	return sc
}

// WithKeepAlive programs the websocket to ping on the specified interval.
// Deprecated: rename to WithWebSocketKeepAlive to avoid confusing with the keep-alive specification of the subscription protocol.
func (sc *SubscriptionClient) WithKeepAlive(interval time.Duration) *SubscriptionClient {
	sc.websocketKeepAliveInterval = interval

	return sc
}

// WithWebSocketKeepAlive programs the websocket to ping on the specified interval.
func (sc *SubscriptionClient) WithWebSocketKeepAlive(interval time.Duration) *SubscriptionClient {
	sc.websocketKeepAliveInterval = interval

	return sc
}

// WithRetryDelay set the delay time before retrying the connection
func (sc *SubscriptionClient) WithRetryDelay(delay time.Duration) *SubscriptionClient {
	sc.retryDelay = delay
	return sc
}

// WithLog sets logging function to print out received messages. By default, nothing is printed
func (sc *SubscriptionClient) WithLog(logger func(args ...interface{})) *SubscriptionClient {
	sc.log = logger

	return sc
}

// WithoutLogTypes these operation types won't be printed
func (sc *SubscriptionClient) WithoutLogTypes(types ...OperationMessageType) *SubscriptionClient {
	sc.disabledLogTypes = types

	return sc
}

// WithReadLimit set max size of response message
func (sc *SubscriptionClient) WithReadLimit(limit int64) *SubscriptionClient {
	sc.readLimit = limit

	return sc
}

// WithRetryStatusCodes allow retry the subscription connection when receiving one of these codes
// the input parameter can be number string or range, e.g 4000-5000
func (sc *SubscriptionClient) WithRetryStatusCodes(codes ...string) *SubscriptionClient {
	statusCodes, err := parseInt32Ranges(codes)
	if err != nil {
		panic(err)
	}

	sc.retryStatusCodes = statusCodes

	return sc
}

// OnError event is triggered when there is any connection error. This is bottom exception handler level
// If this function is empty, or returns nil, the client restarts the connection
// If returns error, the websocket connection will be terminated
func (sc *SubscriptionClient) OnError(onError func(sc *SubscriptionClient, err error) error) *SubscriptionClient {
	sc.onError = onError
	return sc
}

// OnConnected event is triggered when the websocket connected to GraphQL server successfully
func (sc *SubscriptionClient) OnConnected(fn func()) *SubscriptionClient {
	sc.onConnected = fn
	return sc
}

// OnDisconnected event is triggered when the websocket client was disconnected
func (sc *SubscriptionClient) OnDisconnected(fn func()) *SubscriptionClient {
	sc.onDisconnected = fn
	return sc
}

// OnConnectionAlive event is triggered when the websocket receive a connection alive message (differs per protocol)
func (sc *SubscriptionClient) OnConnectionAlive(fn func()) *SubscriptionClient {
	sc.onConnectionAlive = fn
	return sc
}

// OnSubscriptionComplete event is triggered when the subscription receives a terminated message from the server
func (sc *SubscriptionClient) OnSubscriptionComplete(fn func(sub Subscription)) *SubscriptionClient {
	sc.onSubscriptionComplete = fn
	return sc
}

func (sc *SubscriptionClient) getCurrentSession() *SubscriptionContext {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	return sc.currentSession
}

func (sc *SubscriptionClient) setCurrentSession(value *SubscriptionContext) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	sc.currentSession = value
}

// get internal client status
func (sc *SubscriptionClient) getClientStatus() int32 {
	return atomic.LoadInt32(&sc.clientStatus)
}

// set the running atomic lock status
func (sc *SubscriptionClient) setClientStatus(value int32) {
	atomic.StoreInt32(&sc.clientStatus, value)
}

// Subscribe sends start message to server and open a channel to receive data.
// The handler callback function will receive raw message data or error. If the call return error, onError event will be triggered
// The function returns subscription ID and error. You can use subscription ID to unsubscribe the subscription
func (sc *SubscriptionClient) Subscribe(v interface{}, variables map[string]interface{}, handler func(message []byte, err error) error, options ...Option) (string, error) {
	return sc.do(v, variables, handler, options...)
}

// NamedSubscribe sends start message to server and open a channel to receive data, with operation name
//
// Deprecated: this is the shortcut of Subscribe method, with NewOperationName option
func (sc *SubscriptionClient) NamedSubscribe(name string, v interface{}, variables map[string]interface{}, handler func(message []byte, err error) error, options ...Option) (string, error) {
	return sc.do(v, variables, handler, append(options, OperationName(name))...)
}

// SubscribeRaw sends start message to server and open a channel to receive data, with raw query
// Deprecated: use Exec instead
func (sc *SubscriptionClient) SubscribeRaw(query string, variables map[string]interface{}, handler func(message []byte, err error) error) (string, error) {
	return sc.doRaw(query, variables, "", handler)
}

// Exec sends start message to server and open a channel to receive data, with raw query
func (sc *SubscriptionClient) Exec(query string, variables map[string]interface{}, handler func(message []byte, err error) error) (string, error) {
	return sc.doRaw(query, variables, "", handler)
}

func (sc *SubscriptionClient) do(v interface{}, variables map[string]interface{}, handler func(message []byte, err error) error, options ...Option) (string, error) {
	query, operationName, err := ConstructSubscription(v, variables, options...)
	if err != nil {
		return "", err
	}

	return sc.doRaw(query, variables, operationName, handler)
}

func (sc *SubscriptionClient) doRaw(query string, variables map[string]interface{}, operationName string, handler func(message []byte, err error) error) (string, error) {
	id := uuid.New().String()

	sub := Subscription{
		id:  id,
		key: id,
		payload: GraphQLRequestPayload{
			Query:         query,
			Variables:     variables,
			OperationName: operationName,
		},
		handler: sc.wrapHandler(handler),
	}

	sc.mutex.Lock()
	sc.rawSubscriptions[id] = sub
	currentSession := sc.currentSession
	sc.mutex.Unlock()

	if currentSession != nil {
		currentSession.SetSubscription(id, &sub)
		// if the websocket client is running and acknowledged by the server
		// start subscription immediately
		if sc.getClientStatus() == scStatusRunning && currentSession.GetAcknowledge() {
			if err := sc.protocol.Subscribe(currentSession, sub); err != nil {
				return id, err
			}
		}
	}

	return id, nil
}

func (sc *SubscriptionClient) wrapHandler(fn handlerFunc) func(data []byte, err error) {
	return func(data []byte, err error) {
		if errValue := fn(data, err); errValue != nil {
			sc.errorChan <- errValue
		}
	}
}

// Unsubscribe sends stop message to server and close subscription channel
// The input parameter is subscription ID that is returned from Subscribe function
func (sc *SubscriptionClient) Unsubscribe(id string) error {
	sc.mutex.Lock()

	_, ok := sc.rawSubscriptions[id]
	if !ok {
		sc.mutex.Unlock()

		return fmt.Errorf("%s: %w", id, ErrSubscriptionNotExists)
	}

	currentSession := sc.currentSession
	delete(sc.rawSubscriptions, id)
	sc.mutex.Unlock()

	sessionSub := currentSession.GetSubscription(id)
	if currentSession == nil || sessionSub == nil || sessionSub.status == SubscriptionUnsubscribed {
		return nil
	}

	var err error
	if sessionSub.status == SubscriptionRunning {
		err = sc.protocol.Unsubscribe(currentSession, *sessionSub)
	}

	sessionSub.status = SubscriptionUnsubscribed
	currentSession.SetSubscription(sessionSub.key, sessionSub)

	sc.checkSubscriptionStatuses(currentSession)

	return err
}

// create a new subscription context to start a new session
func (sc *SubscriptionClient) initNewSession(ctx context.Context) (*SubscriptionContext, error) {
	// make sure that the current session was closed
	currentSession := sc.getCurrentSession()
	if currentSession != nil {
		_ = currentSession.Close()
	}

	subContext := &SubscriptionContext{
		client:        sc,
		subscriptions: make(map[string]Subscription),
	}

	for key, sub := range sc.rawSubscriptions {
		newSubscription := sub.Clone()
		subContext.SetSubscription(key, &newSubscription)
	}

	if err := subContext.init(ctx); err != nil {
		return nil, fmt.Errorf("retry timeout, %w", err)
	}

	conn := subContext.GetWebsocketConn()
	if conn == nil {
		return nil, fmt.Errorf("the websocket connection hasn't been created")
	}

	if sc.websocketKeepAliveInterval > 0 {
		go subContext.startWebsocketKeepAlive(conn, sc.websocketKeepAliveInterval)
	}

	sc.setCurrentSession(subContext)
	sc.setClientStatus(scStatusRunning)

	return subContext, nil
}

// Run start the WebSocket client and subscriptions.
// If the client is running, recalling this function will return errors.
// If this function is run with goroutine, it can be stopped after closed.
func (sc *SubscriptionClient) Run() error {
	return sc.RunWithContext(context.Background())
}

// RunWithContext start the WebSocket client and subscriptions.
// If the client is running, recalling this function will return errors.
// If this function is run with goroutine, it can be stopped after closed.
func (sc *SubscriptionClient) RunWithContext(ctx context.Context) error {
	if sc.getClientStatus() == scStatusRunning {
		_ = sc.close(sc.getCurrentSession())
	}

	ctx, cancel := context.WithCancel(ctx)
	sc.cancel = cancel

	subContext, err := sc.initNewSession(ctx)
	if err != nil {
		return err
	}

	go subContext.run()

	for {
		select {
		case <-ctx.Done():
			return sc.close(subContext)
		case e := <-sc.errorChan:
			if sc.getClientStatus() == scStatusClosing {
				return nil
			}

			// stop the subscription if the error has stop message
			if errors.Is(e, ErrSubscriptionStopped) {
				return sc.close(subContext)
			}

			if !errors.Is(e, errRetry) && sc.onError != nil {
				// if the user manually catch the error to decide if it can be retried.
				if err := sc.onError(sc, e); err != nil {
					sc.close(subContext)

					return err
				}
			}

			// if the user doesn't manually catch the error
			// the client also automatically retries the connection.
			subContext, err := sc.initNewSession(ctx)
			if err != nil {
				return err
			}

			go subContext.run()
		default:
			session := sc.getCurrentSession()
			if session == nil {
				continue
			}

			if sc.connectionInitialisationTimeout > 0 && !session.GetAcknowledge() &&
				time.Since(session.connectionInitAt) > sc.connectionInitialisationTimeout {
				sc.errorChan <- &websocket.CloseError{
					Code:   StatusConnectionInitialisationTimeout,
					Reason: "Connection initialisation timeout",
				}
			}
		}
	}
}

// Close closes all subscription channel and websocket as well
func (sc *SubscriptionClient) Close() (err error) {
	sc.rawSubscriptions = map[string]Subscription{}

	return sc.close(sc.getCurrentSession())
}

func (sc *SubscriptionClient) close(session *SubscriptionContext) (err error) {
	if sc.getClientStatus() == scStatusClosing {
		return nil
	}

	sc.setClientStatus(scStatusClosing)
	if sc.cancel != nil {
		sc.cancel()
	}

	if session == nil {
		return
	}

	unsubscribeErrors := make(map[string]error)

	conn := session.GetWebsocketConn()
	if conn == nil {
		return
	}

	for key, sub := range session.GetSubscriptions() {
		session.SetSubscription(key, nil)
		if sub.status == SubscriptionRunning {
			if err := sc.protocol.Unsubscribe(session, sub); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, context.Canceled) {
				unsubscribeErrors[key] = err
			}
		}
	}

	protocolCloseError := sc.protocol.Close(session)
	closeError := session.Close()

	if len(unsubscribeErrors) > 0 {
		return Error{
			Message: "failed to close the subscription client",
			Extensions: map[string]interface{}{
				"unsubscribe": unsubscribeErrors,
				"protocol":    protocolCloseError,
				"close":       closeError,
			},
		}
	}

	return nil
}

func (sc *SubscriptionClient) checkSubscriptionStatuses(session *SubscriptionContext) {
	// close the client if there is no running subscription
	if sc.exitWhenNoSubscription && session.GetSubscriptionsLength([]SubscriptionStatus{
		SubscriptionRunning,
		SubscriptionWaiting,
	}) == 0 {
		session.Log("no running subscription. exiting...", "client", GQLInternal)
		sc.close(session)
	}
}

// the reusable function for sending connection init message.
// The payload format of both subscriptions-transport-ws and graphql-ws are the same
func connectionInit(conn *SubscriptionContext, connectionParams map[string]interface{}) error {
	var bParams []byte = nil
	var err error
	if connectionParams != nil {
		bParams, err = json.Marshal(connectionParams)
		if err != nil {
			return err
		}
	}

	// send connection_init event to the server
	msg := OperationMessage{
		Type:    GQLConnectionInit,
		Payload: bParams,
	}

	return conn.Send(msg, GQLConnectionInit)
}

func parseInt32Ranges(codes []string) ([][]int32, error) {
	statusCodes := make([][]int32, 0, len(codes))
	for _, c := range codes {
		sRange := strings.Split(c, "-")
		iRange := make([]int32, len(sRange))
		for j, sCode := range sRange {
			i, err := strconv.ParseInt(sCode, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid status code; input: %s", sCode)
			}
			iRange[j] = int32(i)
		}
		if len(iRange) > 0 {
			statusCodes = append(statusCodes, iRange)
		}
	}

	return statusCodes, nil
}

// default websocket handler implementation using https://github.com/coder/websocket
type WebsocketHandler struct {
	*websocket.Conn

	ctx          context.Context
	readTimeout  time.Duration
	writeTimeout time.Duration
}

// WriteJSON implements the function to encode and send message in json format to the server
func (wh *WebsocketHandler) WriteJSON(v interface{}) error {
	ctx, cancel := context.WithTimeout(wh.ctx, wh.writeTimeout)
	defer cancel()

	return wsjson.Write(ctx, wh.Conn, v)
}

// ReadJSON implements the function to decode the json message from the server
func (wh *WebsocketHandler) ReadJSON(v interface{}) error {
	ctx, cancel := context.WithTimeout(wh.ctx, wh.readTimeout)
	defer cancel()

	return wsjson.Read(ctx, wh.Conn, v)
}

// Ping sends a ping to the peer and waits for a pong
func (wh *WebsocketHandler) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return wh.Conn.Ping(ctx)
}

// Close implements the function to close the websocket connection
func (wh *WebsocketHandler) Close() error {
	return wh.Conn.Close(websocket.StatusNormalClosure, "close websocket")
}

// GetCloseStatus tries to get WebSocket close status from error
// https://www.iana.org/assignments/websocket/websocket.xhtml
func (wh *WebsocketHandler) GetCloseStatus(err error) int32 {
	// context timeout error returned from ReadJSON or WriteJSON
	// try to ping the server, if failed return abnormal closeure error
	if errors.Is(err, context.DeadlineExceeded) {
		if pingErr := wh.Ping(); pingErr != nil {
			return int32(websocket.StatusNoStatusRcvd)
		}
		return -1
	}

	code := websocket.CloseStatus(err)
	if code == -1 && strings.Contains(err.Error(), "received header with unexpected rsv bits") {
		return int32(websocket.StatusNormalClosure)
	}

	return int32(code)
}

// the default constructor function to create a websocket client
// which uses https://github.com/coder/websocket library
func newWebsocketConn(ctx context.Context, endpoint string, options WebsocketOptions) (WebsocketConn, error) {
	opts := &websocket.DialOptions{
		Subprotocols:         options.Subprotocols,
		HTTPClient:           options.HTTPClient,
		HTTPHeader:           options.HTTPHeader,
		Host:                 options.Host,
		CompressionMode:      options.CompressionMode,
		CompressionThreshold: options.CompressionThreshold,
	}

	c, _, err := websocket.Dial(ctx, endpoint, opts)
	if err != nil {
		return nil, err
	}

	return &WebsocketHandler{
		Conn:         c,
		ctx:          ctx,
		readTimeout:  options.ReadTimeout,
		writeTimeout: options.WriteTimeout,
	}, nil
}

// WebsocketOptions allows implementation agnostic configuration of the websocket client
type WebsocketOptions struct {
	// HTTPClient is used for the connection.
	// Its Transport must return writable bodies for WebSocket handshakes.
	// http.Transport does beginning with Go 1.12.
	HTTPClient *http.Client

	// HTTPHeader specifies the HTTP headers included in the handshake request.
	HTTPHeader http.Header

	// Host optionally overrides the Host HTTP header to send. If empty, the value
	// of URL.Host will be used.
	Host string

	// CompressionMode controls the compression mode.
	// Defaults to CompressionDisabled.
	//
	// See docs on CompressionMode for details.
	CompressionMode websocket.CompressionMode

	// CompressionThreshold controls the minimum size of a message before compression is applied.
	//
	// Defaults to 512 bytes for CompressionNoContextTakeover and 128 bytes
	// for CompressionContextTakeover.
	CompressionThreshold int

	// ReadTimeout controls the read timeout of the websocket connection.
	ReadTimeout time.Duration

	// WriteTimeout controls the read timeout of the websocket connection.
	WriteTimeout time.Duration

	// Subprotocols hold subprotocol names of the subscription transport
	// The graphql server depends on the Sec-WebSocket-Protocol header to return the correct message specification
	Subprotocols []string
}
