# go-graphql-client example with Hasura graphql server

## How to run

### Server

Requires [Docker](https://www.docker.com/) and [docker-compose](https://docs.docker.com/compose/install/)

```sh
docker-compose up -d
```

Open the console at `http://localhost:8080` with admin secret `hasura`.

### Client

#### Subscription with subscriptions-transport-ws protocol

```go
go run ./client/subscriptions-transport-ws
```

#### Subscription with graphql-ws protocol

```go
go run ./client/graphql-ws
```
