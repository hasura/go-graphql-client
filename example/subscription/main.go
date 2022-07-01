// subscription is a test program currently being used for developing graphql package.
// It performs queries against a local test GraphQL server instance.
//
// It's not meant to be a clean or readable example. But it's functional.
// Better, actual examples will be created in the future.
package main

func main() {
	go startServer()
	go startSubscription()
	startSendHello()
}
