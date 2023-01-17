# Examples with Hasura graphql server

## How to run

### Server

Requires [Docker](https://www.docker.com/) and [docker-compose](https://docs.docker.com/compose/install/)

```sh
docker-compose up -d
```

Open the console at `http://localhost:8080` with admin secret `hasura`.

### Client

#### Subscription with subscriptions-transport-ws protocol

```sh
go run ./client/subscriptions-transport-ws
```

#### Subscription with graphql-ws protocol

```sh
go run ./client/graphql-ws
```
