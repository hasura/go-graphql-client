module github.com/hasura/go-graphql-client

go 1.20

require (
	github.com/google/uuid v1.5.0
	github.com/graph-gophers/graphql-go v1.5.0
	github.com/graph-gophers/graphql-transport-ws v0.0.2
	nhooyr.io/websocket v1.8.10
)

require (
	github.com/gorilla/websocket v1.5.1 // indirect
	github.com/klauspost/compress v1.17.4 // indirect
	golang.org/x/crypto v0.17.0 // indirect
	golang.org/x/net v0.19.0 // indirect
	golang.org/x/sys v0.16.0 // indirect
)

replace github.com/gin-gonic/gin v1.6.3 => github.com/gin-gonic/gin v1.9.1
