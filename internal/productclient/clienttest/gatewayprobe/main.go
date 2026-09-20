// gatewayprobe exercises the actual desktop gateway sender against a hermetic server fixture.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productruntime"
	"github.com/open-octo/octo-agent/internal/sensitive"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("gateway cross-repository sender and output filtering passed")
}
func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("expected host and model")
	}
	holder := &productclient.CredentialHolder{}
	holder.Set(productclient.Credentials{AccessToken: "fixture-token"})
	endpoint := productruntime.GatewayEndpoint{Host: os.Args[1], Tokens: holder}
	for _, stream := range []bool{false, true} {
		sender, err := endpoint.Sender(app.ReasoningTuning{ClientSessionID: "cross-gateway-task"})
		if err != nil {
			return err
		}
		sender = app.WrapSensitive(sender, sensitive.New(""))
		messages := []agent.Message{{Role: "user", Content: "hello"}}
		var reply agent.Reply
		if stream {
			var chunks strings.Builder
			reply, err = sender.(agent.StreamingSender).StreamMessages(context.Background(), os.Args[2], "system", messages, 128, func(s string) { chunks.WriteString(s) }, func(string) {})
			if err == nil && chunks.String() != "增值税***管理" {
				return fmt.Errorf("unfiltered streamed output: %q", chunks.String())
			}
		} else {
			reply, err = sender.SendMessages(context.Background(), os.Args[2], "system", messages, 128)
		}
		if err != nil {
			return err
		}
		if reply.Content != "增值税***管理" {
			return fmt.Errorf("unfiltered reply: %q", reply.Content)
		}
	}
	return nil
}
