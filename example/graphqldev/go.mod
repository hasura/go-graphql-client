module github.com/hasura/go-graphql-client/example/graphqldev

go 1.20

require (
	github.com/graph-gophers/graphql-go v1.5.0
	github.com/hasura/go-graphql-client v0.11.0
)

require (
	github.com/coder/websocket v1.8.12 // indirect
	github.com/google/uuid v1.6.0 // indirect
)

replace github.com/hasura/go-graphql-client => ../../
