# go-graphql-client

[![Unit tests](https://github.com/hasura/go-graphql-client/actions/workflows/test.yml/badge.svg)](https://github.com/hasura/go-graphql-client/actions/workflows/test.yml)

**Preface:** This is a fork of `https://github.com/shurcooL/graphql` with extended features (subscription client, named operation)

The subscription client follows Apollo client specification https://github.com/apollographql/subscriptions-transport-ws/blob/master/PROTOCOL.md, using WebSocket protocol with https://github.com/coder/websocket, a minimal and idiomatic WebSocket library for Go.

Package `graphql` provides a GraphQL client implementation.

**Note**: Before v0.8.0, `QueryRaw`, `MutateRaw`, and `Subscribe` methods return `*json.RawMessage`. This output type is redundant to be decoded. From v0.8.0, the output type is changed to `[]byte`.

- [go-graphql-client](#go-graphql-client)
	- [Installation](#installation)
	- [Usage](#usage)
		- [Authentication](#authentication)
			- [WithRequestModifier](#withrequestmodifier)
			- [OAuth2](#oauth2)
		- [Simple Query](#simple-query)
		- [Arguments and Variables](#arguments-and-variables)
		- [Custom scalar tag](#custom-scalar-tag)
		- [Skip GraphQL field](#skip-graphql-field)
		- [Inline Fragments](#inline-fragments)
		- [Specify GraphQL type name](#specify-graphql-type-name)
		- [Mutations](#mutations)
			- [Mutations Without Fields](#mutations-without-fields)
		- [Retry Options](#retry-options)
		- [Subscription](#subscription)
			- [Usage](#usage-1)
			- [Subscribe](#subscribe)
			- [Stop the subscription](#stop-the-subscription)
			- [Authentication](#authentication-1)
			- [Options](#options)
			- [Subscription Protocols](#subscription-protocols)
			- [Handle connection error](#handle-connection-error)
				- [Connection Initialisation Timeout](#connection-initialisation-timeout)
				- [WebSocket Connection Idle Timeout](#websocket-connection-idle-timeout)
			- [Events](#events)
			- [Custom HTTP Client](#custom-http-client)
			- [Custom WebSocket client](#custom-websocket-client)
		- [Options](#options-1)
		- [Execute pre-built query](#execute-pre-built-query)
		- [Get extensions from response](#get-extensions-from-response)
		- [Get headers from response](#get-headers-from-response)
		- [With operation name (deprecated)](#with-operation-name-deprecated)
		- [Raw bytes response](#raw-bytes-response)
		- [Multiple mutations with ordered map](#multiple-mutations-with-ordered-map)
		- [Debugging and Unit test](#debugging-and-unit-test)
	- [Directories](#directories)
	- [References](#references)
	- [License](#license)

## Installation

`go-graphql-client` requires Go version 1.20 or later. For older Go versions:

- **>= 1.16 < 1.20**: downgrade the library to version v0.9.x
- **< 1.16**: downgrade the library version below v0.7.1.

```bash
go get -u github.com/hasura/go-graphql-client
```

## Usage

Construct a GraphQL client, specifying the GraphQL server URL. Then, you can use it to make GraphQL queries and mutations.

```Go
client := graphql.NewClient("https://example.com/graphql", nil)
// Use client...
```

### Authentication

Some GraphQL servers may require authentication. The `graphql` package does not directly handle authentication. Instead, when creating a new client, you're expected to pass an `http.Client` that performs authentication.

#### WithRequestModifier

Use `WithRequestModifier` method to inject headers into the request before sending to the GraphQL server.

```go
client := graphql.NewClient(endpoint, http.DefaultClient).
  WithRequestModifier(func(r *http.Request) {
	  r.Header.Set("Authorization", "random-token")
  })
```

#### OAuth2

The easiest and recommended way to do this is to use the [`golang.org/x/oauth2`](https://golang.org/x/oauth2) package. You'll need an OAuth token with the right scopes. Then:

```Go
import "golang.org/x/oauth2"

func main() {
	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("GRAPHQL_TOKEN")},
	)
	httpClient := oauth2.NewClient(context.Background(), src)

	client := graphql.NewClient("https://example.com/graphql", httpClient)
	// Use client...
```

### Simple Query

To make a GraphQL query, you need to define a corresponding Go type. Variable names must be upper case, see [here](https://github.com/hasura/go-graphql-client/blob/master/README.md#specify-graphql-type-name)

For example, to make the following GraphQL query:

```GraphQL
query {
	me {
		name
	}
}
```

You can define this variable:

```Go
var query struct {
	Me struct {
		Name string
	}
}
```

Then call `client.Query`, passing a pointer to it:

```Go
err := client.Query(context.Background(), &query, nil)
if err != nil {
	// Handle error.
}
fmt.Println(query.Me.Name)

// Output: Luke Skywalker
```

### Arguments and Variables

Often, you'll want to specify arguments on some fields. You can use the `graphql` struct field tag for this.

For example, to make the following GraphQL query:

```GraphQL
{
	human(id: "1000") {
		name
		height(unit: METER)
	}
}
```

You can define this variable:

```Go
var q struct {
	Human struct {
		Name   string
		Height float64 `graphql:"height(unit: METER)"`
	} `graphql:"human(id: \"1000\")"`
}
```

Then call `client.Query`:

```Go
err := client.Query(context.Background(), &q, nil)
if err != nil {
	// Handle error.
}
fmt.Println(q.Human.Name)
fmt.Println(q.Human.Height)

// Output:
// Luke Skywalker
// 1.72
```

However, that'll only work if the arguments are constant and known in advance. Otherwise, you will need to make use of variables. Replace the constants in the struct field tag with variable names:

```Go
var q struct {
	Human struct {
		Name   string
		Height float64 `graphql:"height(unit: $unit)"`
	} `graphql:"human(id: $id)"`
}
```

Then, define a `variables` map with their values:

```Go
variables := map[string]interface{}{
	"id":   graphql.ID(id),
	"unit": starwars.LengthUnit("METER"),
}
```

Finally, call `client.Query` providing `variables`:

```Go
err := client.Query(context.Background(), &q, variables)
if err != nil {
	// Handle error.
}
```

Variables get encoded as normal JSON. So if you supply a struct for a variable and want to rename fields, you can do this like this:

```Go
type Dimensions struct {
	Width int `json:"ship_width"`,
	Height int `json:"ship_height"`
}

var myDimensions = Dimensions{
	Width : 10,
	Height : 6,
}

var mutation struct {
  CreateDimensions struct {
     ID string `graphql:"id"`
  } `graphql:"create_dimensions(ship_dimensions: $ship_dimensions)"`
}

variables := map[string]interface{}{
	"ship_dimensions":  myDimensions,
}

err := client.Mutate(context.TODO(), &mutation, variables)

```

which will set `ship_dimensions` to an object with the properties `ship_width` and `ship_height`.

### Custom scalar tag

Because the generator reflects recursively struct objects, it can't know if the struct is a custom scalar such as JSON. To avoid expansion of the field during query generation, let's add the tag `scalar:"true"` to the custom scalar. If the scalar implements the JSON decoder interface, it will be automatically decoded.

```Go
struct {
	Viewer struct {
		ID         interface{}
		Login      string
		CreatedAt  time.Time
		DatabaseID int
	}
}

// Output:
// {
//   viewer {
//	   id
//		 login
//		 createdAt
//		 databaseId
//   }
// }

struct {
	Viewer struct {
		ID         interface{}
		Login      string
		CreatedAt  time.Time
		DatabaseID int
	} `scalar:"true"`
}

// Output
// { viewer }
```

### Skip GraphQL field

```go
struct {
  Viewer struct {
		ID         interface{} `graphql:"-"`
		Login      string
		CreatedAt  time.Time `graphql:"-"`
		DatabaseID int
  }
}

// Output
// {viewer{login,databaseId}}
```

### Inline Fragments

Some GraphQL queries contain inline fragments. You can use the `graphql` struct field tag to express them.

For example, to make the following GraphQL query:

```GraphQL
{
	hero(episode: "JEDI") {
		name
		... on Droid {
			primaryFunction
		}
		... on Human {
			height
		}
	}
}
```

You can define this variable:

```Go
var q struct {
	Hero struct {
		Name  string
		Droid struct {
			PrimaryFunction string
		} `graphql:"... on Droid"`
		Human struct {
			Height float64
		} `graphql:"... on Human"`
	} `graphql:"hero(episode: \"JEDI\")"`
}
```

Alternatively, you can define the struct types corresponding to inline fragments, and use them as embedded fields in your query:

```Go
type (
	DroidFragment struct {
		PrimaryFunction string
	}
	HumanFragment struct {
		Height float64
	}
)

var q struct {
	Hero struct {
		Name          string
		DroidFragment `graphql:"... on Droid"`
		HumanFragment `graphql:"... on Human"`
	} `graphql:"hero(episode: \"JEDI\")"`
}
```

Then call `client.Query`:

```Go
err := client.Query(context.Background(), &q, nil)
if err != nil {
	// Handle error.
}
fmt.Println(q.Hero.Name)
fmt.Println(q.Hero.PrimaryFunction)
fmt.Println(q.Hero.Height)

// Output:
// R2-D2
// Astromech
// 0
```

### Specify GraphQL type name

The GraphQL type is automatically inferred from Go type by reflection. However, it's cumbersome in some use cases, e.g. lowercase names. In Go, a type name with a first lowercase letter is considered private. If we need to reuse it for other packages, there are 2 approaches: type alias or implement `GetGraphQLType` method.

```go
type UserReviewInput struct {
	Review string
	UserID string
}

// type alias
type user_review_input UserReviewInput
// or implement GetGraphQLType method
func (u UserReviewInput) GetGraphQLType() string { return "user_review_input" }

variables := map[string]interface{}{
  "input": UserReviewInput{}
}

//query arguments without GetGraphQLType() defined
//($input: UserReviewInput!)
//query arguments with GetGraphQLType() defined:w
//($input: user_review_input!)
```

### Mutations

Mutations often require information that you can only find out by performing a query first. Let's suppose you've already done that.

For example, to make the following GraphQL mutation:

```GraphQL
mutation($ep: Episode!, $review: ReviewInput!) {
	createReview(episode: $ep, review: $review) {
		stars
		commentary
	}
}
variables {
	"ep": "JEDI",
	"review": {
		"stars": 5,
		"commentary": "This is a great movie!"
	}
}
```

You can define:

```Go
var m struct {
	CreateReview struct {
		Stars      int
		Commentary string
	} `graphql:"createReview(episode: $ep, review: $review)"`
}
variables := map[string]interface{}{
	"ep": starwars.Episode("JEDI"),
	"review": starwars.ReviewInput{
		Stars:      5,
		Commentary: "This is a great movie!",
	},
}
```

Then call `client.Mutate`:

```Go
err := client.Mutate(context.Background(), &m, variables)
if err != nil {
	// Handle error.
}
fmt.Printf("Created a %v star review: %v\n", m.CreateReview.Stars, m.CreateReview.Commentary)

// Output:
// Created a 5 star review: This is a great movie!
```

#### Mutations Without Fields

Sometimes, you don't need any fields returned from a mutation. Doing that is easy.

For example, to make the following GraphQL mutation:

```GraphQL
mutation($ep: Episode!, $review: ReviewInput!) {
	createReview(episode: $ep, review: $review)
}
variables {
	"ep": "JEDI",
	"review": {
		"stars": 5,
		"commentary": "This is a great movie!"
	}
}
```

You can define:

```Go
var m struct {
	CreateReview string `graphql:"createReview(episode: $ep, review: $review)"`
}
variables := map[string]interface{}{
	"ep": starwars.Episode("JEDI"),
	"review": starwars.ReviewInput{
		Stars:      5,
		Commentary: "This is a great movie!",
	},
}
```

Then call `client.Mutate`:

```Go
err := client.Mutate(context.Background(), &m, variables)
if err != nil {
	// Handle error.
}
fmt.Printf("Created a review: %s.\n", m.CreateReview)

// Output:
// Created a review: .
```

### Retry Options

Construct the client with the following options:

```go
client := graphql.NewClient("/graphql", http.DefaultClient,
	// number of retries
	graphql.WithRetry(3),
	// base backoff interval. Optional, default 1 second.
	// Prioritize the Retry-After header if it exists in the response.
	graphql.WithRetryBaseDelay(time.Second),
	// exponential rate. Optional, default 2.0
	graphql.WithRetryExponentialRate(2),
	// retry on http statuses. Optional, default: 429, 502, 503, 504
	graphql.WithRetryHTTPStatus([]int{http.StatusServiceUnavailable}),
	// if the http status is 200 but the graphql response is error, 
	// use this option to check if the error is retryable.
	graphql.WithRetryOnGraphQLError(func(errs graphql.Errors) bool {
		return len(errs) == 1 && errs[0].Message == "Field 'user' is missing required arguments: login"
	}),
)
```

### Subscription

#### Usage

Construct a Subscription client, specifying the GraphQL server URL.

```Go
client := graphql.NewSubscriptionClient("wss://example.com/graphql")
defer client.Close()

// Subscribe subscriptions
// ...
// finally run the client
client.Run()
```

#### Subscribe

To make a GraphQL subscription, you need to define a corresponding Go type.

For example, to make the following GraphQL query:

```GraphQL
subscription {
	me {
		name
	}
}
```

You can define this variable:

```Go
var subscription struct {
	Me struct {
		Name string
	}
}
```

Then call `client.Subscribe`, passing a pointer to it:

```Go
subscriptionId, err := client.Subscribe(&query, nil, func(dataValue []byte, errValue error) error {
	if errValue != nil {
		// handle error
		// if returns error, it will failback to `onError` event
		return nil
	}
	data := query{}
	// use the github.com/hasura/go-graphql-client/pkg/jsonutil package
	err := jsonutil.UnmarshalGraphQL(dataValue, &data)

	fmt.Println(query.Me.Name)

	// Output: Luke Skywalker
	return nil
})

if err != nil {
	// Handle error.
}
```

#### Stop the subscription

You can programmatically stop the subscription while the client is running by using the `Unsubscribe` method or returning a special error to stop it in the callback.

```Go
subscriptionId, err := client.Subscribe(&query, nil, func(dataValue []byte, errValue error) error {
	// ...
	// return this error to stop the subscription in the callback
	return graphql.ErrSubscriptionStopped
})

if err != nil {
	// Handle error.
}

// unsubscribe the subscription while the client is running with the subscription ID
client.Unsubscribe(subscriptionId)
```

#### Authentication

The subscription client is authenticated with GraphQL server through connection params:

```Go
client := graphql.NewSubscriptionClient("wss://example.com/graphql").
	WithConnectionParams(map[string]interface{}{
		"headers": map[string]string{
				"authentication": "...",
		},
	}).
	// or lazy parameters with function
  WithConnectionParamsFn(func () map[string]interface{} {
		return map[string]interface{} {
			"headers": map[string]string{
  				"authentication": "...",
  		},
		}
	})
```

Some servers validate custom auth tokens on the header instead. To authenticate with headers, use `WebsocketOptions`:

```go
client := graphql.NewSubscriptionClient(serverEndpoint).
    WithWebSocketOptions(graphql.WebsocketOptions{
        HTTPHeader: http.Header{
            "Authorization": []string{"Bearer random-secret"},
        },
    })
```

#### Options

```Go
client.
	//  write timeout of websocket client
	WithTimeout(time.Minute).
	// When the websocket server was stopped, the client will retry connecting every second until timeout
	WithRetryTimeout(time.Minute).
	// sets loging function to print out received messages. By default, nothing is printed
	WithLog(log.Println).
	// max size of response message
	WithReadLimit(10*1024*1024).
	// these operation event logs won't be printed
	WithoutLogTypes(graphql.GQLData, graphql.GQLConnectionKeepAlive).
	// the client should exit when all subscriptions were closed, default true
	WithExitWhenNoSubscription(false).
	// WithRetryStatusCodes allow retry the subscription connection when receiving one of these codes
	// the input parameter can be number string or range, e.g 4000-5000
	WithRetryStatusCodes("4000", "4000-4050").
	// WithSyncMode subscription messages are executed in sequence (without goroutine)
	WithSyncMode(true)
```

#### Subscription Protocols

The subscription client supports 2 protocols:

- [subscriptions-transport-ws](https://github.com/apollographql/subscriptions-transport-ws/blob/master/PROTOCOL.md) (default)
- [graphql-ws](https://github.com/enisdenjo/graphql-ws/blob/master/PROTOCOL.md)

The protocol can be switchable by the `WithProtocol` function.

```Go
client.WithProtocol(graphql.GraphQLWS)
```

#### Handle connection error

GraphQL servers can define custom WebSocket error codes in the 3000-4999 range. For example, in the `graphql-ws` protocol, the server sends the invalid message error with status [4400](https://github.com/enisdenjo/graphql-ws/blob/master/PROTOCOL.md#invalid-message). In this case, the subscription client should let the user handle the error through the `OnError` event.

```go
client := graphql.NewSubscriptionClient(serverEndpoint).
  OnError(func(sc *graphql.SubscriptionClient, err error) error {
  	if sc.IsUnauthorized(err) || strings.Contains(err.Error(), "invalid x-hasura-admin-secret/x-hasura-access-key") {
			// exit the subscription client due to unauthorized error
  		return err
  	}

		if sc.IsInternalConnectionError(err) {
			return err
		}

		// otherwise ignore the error and the client will restart.
  	return nil
  })
```

##### Connection Initialisation Timeout

The connection initialisation timeout error happens when the subscription client emitted the `ConnectionInit` event but hasn't received any message for a long duration. The default timeout is a minute. You can adjust the timeout by calling the `WithConnectionInitialisationTimeout` method. This error is disabled if the timeout duration is `0`.

```go
client := graphql.NewSubscriptionClient(serverEndpoint).
	WithConnectionInitialisationTimeout(2*time.Minute).
  OnError(func(sc *graphql.SubscriptionClient, err error) error {
  	if sc.IsConnectionInitialisationTimeout(err) {
			// restart the client
  		return nil
  	}

		// catch other errors...

		return err
  })
```

##### WebSocket Connection Idle Timeout

This error happens if the websocket connection idle timeout duration is larger than `0` and the subscription client doesn't receive any message from the server, include keep-alive message for a long duration. The setting is disabled by default and can be configured by the `WithWebsocketConnectionIdleTimeout` method.

```go
client := graphql.NewSubscriptionClient(serverEndpoint).
	WithWebsocketConnectionIdleTimeout(time.Minute).
  OnError(func(sc *graphql.SubscriptionClient, err error) error {
  	if sc.IsWebsocketConnectionIdleTimeout(err) {
			// restart the client
  		return nil
  	}

		// catch other errors...

		return err
  })
```

#### Events

```Go
// OnConnected event is triggered when the websocket connected to GraphQL server sucessfully
client.OnConnected(fn func())

// OnDisconnected event is triggered when the websocket client was disconnected
client.OnDisconnected(fn func())

// OnError event is triggered when there is any connection error. This is bottom exception handler level
// If this function is empty, or returns nil, the error is ignored
// If returns error, the websocket connection will be terminated
client.OnError(onError func(sc *SubscriptionClient, err error) error)

// OnConnectionAlive event is triggered when the websocket receive a connection alive message (differs per protocol)
client.OnConnectionAlive(fn func())

// OnSubscriptionComplete event is triggered when the subscription receives a terminated message from the server
client.OnSubscriptionComplete(fn func(sub Subscription))
```

#### Custom HTTP Client

Use `WithWebSocketOptions` to customize the HTTP client which is used by the subscription client.

```go
client.WithWebSocketOptions(WebsocketOptions{
	HTTPClient: &http.Client{
		Transport: http.DefaultTransport,
		Timeout: time.Minute,
	}
})
```

#### Custom WebSocket client

By default, the subscription client uses [coder WebSocket client](https://github.com/coder/websocket). If you need to customize the client or prefer using [Gorilla WebSocket](https://github.com/gorilla/websocket), let's follow the WebSocket interface and replace the constructor with `WithWebSocket` method:

```go
// WebsocketHandler abstracts WebSocket connection functions
// ReadJSON and WriteJSON data of a frame from the WebSocket connection.
// Close the WebSocket connection.
type WebsocketConn interface {
	ReadJSON(v interface{}) error
	WriteJSON(v interface{}) error
	Close() error
	// SetReadLimit sets the maximum size in bytes for a message read from the peer. If a
	// message exceeds the limit, the connection sends a close message to the peer
	// and returns ErrReadLimit to the application.
	SetReadLimit(limit int64)
}

// WithWebSocket replaces customized websocket client constructor
func (sc *SubscriptionClient) WithWebSocket(fn func(sc *SubscriptionClient) (WebsocketConn, error)) *SubscriptionClient
```

**Example**

```Go

// the default websocket constructor
func newWebsocketConn(sc *SubscriptionClient) (WebsocketConn, error) {
	options := &websocket.DialOptions{
		Subprotocols: []string{"graphql-ws"},
	}
	c, _, err := websocket.Dial(sc.GetContext(), sc.GetURL(), options)
	if err != nil {
		return nil, err
	}

	// The default WebsocketHandler implementation using coder's
	return &WebsocketHandler{
		ctx:     sc.GetContext(),
		Conn:    c,
		timeout: sc.GetTimeout(),
	}, nil
}

client := graphql.NewSubscriptionClient("wss://example.com/graphql")
defer client.Close()

client.WithWebSocket(newWebsocketConn)

client.Run()
```

### Options

There are extensible parts in the GraphQL query that we sometimes use. They are optional so we shouldn't require them in the method. To make it flexible, we can abstract these options as optional arguments that follow this interface.

```go
type Option interface {
	Type() OptionType
	String() string
}

client.Query(ctx context.Context, q interface{}, variables map[string]interface{}, options ...Option) error
```

Currently, there are 4 option types:

- `operation_name`
- `operation_directive`
- `bind_extensions`
- `bind_response_headers`

The operation name option is built-in because it is unique. We can use the option directly with `OperationName`.

```go
// query MyQuery {
//	...
// }
client.Query(ctx, &q, variables, graphql.OperationName("MyQuery"))
```

In contrast, operation directives are various and customizable on different GraphQL servers. There isn't any built-in directive in the library. You need to define yourself. For example:

```go
// define @cached directive for Hasura queries
// https://hasura.io/docs/latest/graphql/cloud/response-caching.html#enable-caching
type cachedDirective struct {
	ttl int
}

func (cd cachedDirective) Type() OptionType {
	// operation_directive
	return graphql.OptionTypeOperationDirective
}

func (cd cachedDirective) String() string {
	if cd.ttl <= 0 {
		return "@cached"
	}
	return fmt.Sprintf("@cached(ttl: %d)", cd.ttl)
}

// query MyQuery @cached {
//	...
// }
client.Query(ctx, &q, variables, graphql.OperationName("MyQuery"), cachedDirective{})
```

### Execute pre-built query

The `Exec` function allows you to execute pre-built queries. While using reflection to build queries is convenient as you get some resemblance of type safety, it gets very cumbersome when you need to create queries semi-dynamically. For instance, imagine you are building a CLI tool to query data from a graphql endpoint and you want users to be able to narrow down the query by passing CLI flags or something.

```Go
// filters would be built dynamically somehow from the command line flags
filters := []string{
   `fieldA: {subfieldA: {_eq: "a"}}`,
   `fieldB: {_eq: "b"}`,
   ...
}

query := "query{something(where: {" + strings.Join(filters, ", ") + "}){id}}"
res := struct {
	Somethings []Something
}{}

if err := client.Exec(ctx, query, &res, map[string]any{}); err != nil {
	panic(err)
}

subscription := "subscription{something(where: {" + strings.Join(filters, ", ") + "}){id}}"
subscriptionId, err := subscriptionClient.Exec(subscription, nil, func(dataValue []byte, errValue error) error {
	if errValue != nil {
		// handle error
		// if returns error, it will failback to `onError` event
		return nil
	}
	data := query{}
	err := json.Unmarshal(dataValue, &data)
	// ...
})
```

If you prefer decoding JSON yourself, use `ExecRaw` instead.

```Go
query := `query{something(where: { foo: { _eq: "bar" }}){id}}`
var res struct {
	Somethings []Something `json:"something"`
}

raw, err := client.ExecRaw(ctx, query, map[string]any{})
if err != nil {
	panic(err)
}

err = json.Unmarshal(raw, &res)
```

### Get extensions from response

The response map may also contain an entry with the `extensions` key. To decode this field you need to bind a struct or map pointer. The client will optionally unmarshal the field using JSON decoder.

```go
var q struct {
	User struct {
		ID   string `graphql:"id"`
		Name string `graphql:"name"`
	}
}

var ext struct {
	ID     int    `json:"id"`
	Domain string `json:"domain"`
}

err := client.Query(context.Background(), &q, map[string]interface{}{}, graphql.BindExtensions(&ext))
if err != nil {
	t.Fatal(err)
}
```

Additionally, if you need information about the extensions returned in the response use `ExecRawWithExtensions`. This function returns a map with extensions as the second variable.

```Go
query := `query{something(where: { foo: { _eq: "bar" }}){id}}`

data, extensions, err := client.ExecRawWithExtensions(ctx, query, map[string]any{})
if err != nil {
	panic(err)
}

// You can now use the `extensions` variable to access the extensions data
fmt.Println("Extensions:", extensions)
```

### Get headers from response

Use the `BindResponseHeaders` option to bind headers from the response.

```go
headers := http.Header{}
err := client.Query(context.TODO(), &q, map[string]any{}, graphql.BindResponseHeaders(&headers))
if err != nil {
  panic(err)
}

fmt.Println(headers.Get("content-type"))
// application/json
```

### With operation name (deprecated)

```Go
func (c *Client) NamedQuery(ctx context.Context, name string, q interface{}, variables map[string]interface{}) error

func (c *Client) NamedMutate(ctx context.Context, name string, q interface{}, variables map[string]interface{}) error

func (sc *SubscriptionClient) NamedSubscribe(name string, v interface{}, variables map[string]interface{}, handler func(message []byte, err error) error) (string, error)
```

### Raw bytes response

In the case when we developers want to decode JSON response ourselves. Moreover, the default `UnmarshalGraphQL` function isn't ideal with complicated nested interfaces

```Go
func (c *Client) QueryRaw(ctx context.Context, q interface{}, variables map[string]interface{}) ([]byte, error)

func (c *Client) MutateRaw(ctx context.Context, q interface{}, variables map[string]interface{}) ([]byte, error)

func (c *Client) NamedQueryRaw(ctx context.Context, name string, q interface{}, variables map[string]interface{}) ([]byte, error)

func (c *Client) NamedMutateRaw(ctx context.Context, name string, q interface{}, variables map[string]interface{}) ([]byte, error)
```

### Multiple mutations with ordered map

You might need to make multiple mutations in a single query. It's not very convenient with structs
so you can use ordered map `[][2]interface{}` instead.

For example, to make the following GraphQL mutation:

```GraphQL
mutation($login1: String!, $login2: String!, $login3: String!) {
	createUser(login: $login1) { login }
	createUser(login: $login2) { login }
	createUser(login: $login3) { login }
}
variables {
	"login1": "grihabor",
	"login2": "diman",
	"login3": "indigo"
}
```

You can define:

```Go
type CreateUser struct {
	Login string
}
m := [][2]interface{}{
	{"createUser(login: $login1)", &CreateUser{}},
	{"createUser(login: $login2)", &CreateUser{}},
	{"createUser(login: $login3)", &CreateUser{}},
}
variables := map[string]interface{}{
	"login1": "grihabor",
	"login2": "diman",
	"login3": "indigo",
}
```

### Debugging and Unit test

Enable debug mode with the `WithDebug` function. If the request fails, the request and response information will be included in `extensions[].internal` property.

```json
{
  "errors": [
    {
      "message": "Field 'user' is missing required arguments: login",
      "extensions": {
        "internal": {
          "request": {
            "body": "{\"query\":\"{user{name}}\"}",
            "headers": {
              "Content-Type": ["application/json"]
            }
          },
          "response": {
            "body": "{\"errors\": [{\"message\": \"Field 'user' is missing required arguments: login\",\"locations\": [{\"line\": 7,\"column\": 3}]}]}",
            "headers": {
              "Content-Type": ["application/json"]
            }
          }
        }
      },
      "locations": [
        {
          "line": 7,
          "column": 3
        }
      ]
    }
  ]
}
```

For debugging queries, you can use `Construct*` functions to see what the generated query looks like:

```go
// ConstructQuery build GraphQL query string from struct and variables
func ConstructQuery(v interface{}, variables map[string]interface{}, options ...Option) (string, error)

// ConstructMutation build GraphQL mutation string from struct and variables
func ConstructMutation(v interface{}, variables map[string]interface{}, options ...Option) (string, error)

// ConstructSubscription build GraphQL subscription string from struct and variables
func ConstructSubscription(v interface{}, variables map[string]interface{}, options ...Option) (string, string, error)

// UnmarshalGraphQL parses the JSON-encoded GraphQL response data and stores
// the result in the GraphQL query data structure pointed to by v.
func UnmarshalGraphQL(data []byte, v interface{}) error
```

Because the GraphQL query string is generated in runtime using reflection, it isn't really safe. To ensure the GraphQL query is expected, it's necessary to write some unit tests for query construction.

## Directories

| Path                                                                                   | Synopsis                                                                                                         |
| -------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| [example/graphqldev](https://godoc.org/github.com/shurcooL/graphql/example/graphqldev) | graphqldev is a test program currently being used for developing graphql package.                                |
| [ident](https://godoc.org/github.com/shurcooL/graphql/ident)                           | Package ident provides functions for parsing and converting identifier names between various naming conventions. |
| [internal/jsonutil](https://godoc.org/github.com/shurcooL/graphql/internal/jsonutil)   | Package jsonutil provides a function for decoding JSON into a GraphQL query data structure.                      |

## References

- https://github.com/shurcooL/graphql
- https://github.com/apollographql/subscriptions-transport-ws/blob/master/PROTOCOL.md
- https://github.com/coder/websocket

## License

- [MIT License](LICENSE)
