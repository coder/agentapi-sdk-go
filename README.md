# agentapi-sdk-go

[![GoDoc](https://pkg.go.dev/badge/github.com/coder/agentapi-sdk-go.svg)](https://pkg.go.dev/github.com/coder/agentapi-sdk-go)

Go SDK for the [AgentAPI](https://github.com/coder/agentapi) HTTP API.

## Install

```sh
go get github.com/coder/agentapi-sdk-go
```

## Usage

```go
package main

import (
	"context"
	"fmt"
	"log"

	agentapisdk "github.com/coder/agentapi-sdk-go"
)

func main() {
	ctx := context.Background()

	client, err := agentapisdk.NewClient("http://localhost:3284")
	if err != nil {
		log.Fatal(err)
	}

	// Send a message to the agent.
	_, err = client.PostMessage(ctx, agentapisdk.PostMessageParams{
		Content: "Hello, agent!",
		Type:    agentapisdk.MessageTypeUser,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Check the agent's status.
	status, err := client.GetStatus(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Agent status:", status.Status)

	// Subscribe to server-sent events.
	events, errCh, err := client.SubscribeEvents(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			switch e := ev.(type) {
			case agentapisdk.EventMessageUpdate:
				fmt.Println("Message update:", e.Message)
			case agentapisdk.EventStatusChange:
				fmt.Println("Status change:", e.Status)
			}
		case err := <-errCh:
			log.Fatal(err)
		}
	}
}
```

## Code generation

The `gen/` package is auto-generated from the OpenAPI spec using
[oapi-codegen](https://github.com/oapi-codegen/oapi-codegen). See
[`gen/README.md`](gen/README.md) for the exact command.

## AgentAPI

See the [AgentAPI](https://github.com/coder/agentapi) repository for the
full API specification and server documentation.

## License

[MIT](LICENSE)
