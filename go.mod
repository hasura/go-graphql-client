module github.com/hasura/go-graphql-client

go 1.20

require (
	github.com/google/uuid v1.3.0
	github.com/graph-gophers/graphql-go v1.5.0
	github.com/graph-gophers/graphql-transport-ws v0.0.2
	nhooyr.io/websocket v1.8.7
)

require (
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/klauspost/compress v1.16.7 // indirect
	golang.org/x/crypto v0.0.0-20220314234659-1baeb1ce4c0b // indirect
	golang.org/x/sys v0.0.0-20220412211240-33da011f77ad // indirect
)

replace github.com/gin-gonic/gin v1.6.3 => github.com/gin-gonic/gin v1.7.7
