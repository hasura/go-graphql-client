# Subscription example with graphql-ws backwards compatibility

The example demonstrates the subscription client with the native graphql-ws Node.js server, using [ws server usage with subscriptions-transport-ws backwards compatibility](https://the-guild.dev/graphql/ws/recipes#ws-server-usage-with-subscriptions-transport-ws-backwards-compatibility) and [custom auth handling](https://the-guild.dev/graphql/ws/recipes#server-usage-with-ws-and-custom-auth-handling) recipes. The client authenticates with the server via HTTP header.

```go
client := graphql.NewSubscriptionClient(serverEndpoint).
		WithWebSocketOptions(graphql.WebsocketOptions{
			HTTPHeader: http.Header{
				"Authorization": []string{"Bearer random-secret"},
			},
		})
```

## Get started

### Server

Requires Node.js and npm

```bash
cd server
npm install
npm start
```

The server will be hosted on `localhost:4000`.

### Client

```bash
go run ./client
```

