package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/app"
	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/productruntime"
	"github.com/open-octo/octo-agent/internal/productstate"
	"github.com/open-octo/octo-agent/internal/tools"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("published expert/skills and selected gateway execution passed")
}
func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("expected fixture URL and user-selected model")
	}
	ctx := context.Background()
	tokens := &productclient.CredentialHolder{}
	tokens.Set(productclient.Credentials{AccessToken: "isolated-fixture"})
	client := productclient.New(os.Args[1]+"/api/v1", productclient.ClientMeta{}, tokens)
	experts, err := client.Experts(ctx)
	if err != nil {
		return err
	}
	skills, err := client.Skills(ctx)
	if err != nil {
		return err
	}
	if len(skills.Skills) != 1 || skills.Skills[0].Content != "" {
		return fmt.Errorf("published skill contract mismatch: %+v", skills)
	}
	var selected *productclient.PlatformExpert
	for i := range experts.Experts {
		if experts.Experts[i].Code == "cross-expert" {
			selected = &experts.Experts[i]
		}
	}
	if selected == nil || len(selected.Skills) != 1 || selected.Skills[0].ID != skills.Skills[0].ID {
		return fmt.Errorf("expert association missing")
	}
	root, err := os.MkdirTemp("", "expert-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	if err = os.Setenv("OCTO_DATA_ROOT", root); err != nil {
		return err
	}
	state, err := productstate.Open(productstate.Options{})
	if err != nil {
		return err
	}
	if err = state.ApplyLogin(productstate.LoginOutcome{}); err != nil {
		return err
	}
	runtime := productruntime.New(productruntime.Deps{State: state, Platform: client})
	ctx = tools.WithPlatformSkillLoader(ctx, func(ctx context.Context, id string) (string, error) {
		return runtime.LoadPlatformSkill(ctx, selected.ID, selected.Version, id)
	})
	sender, err := (productruntime.GatewayEndpoint{Host: os.Args[1], Tokens: tokens}).Sender(app.ReasoningTuning{ClientSessionID: "expert-cross-task", ClientAgentID: "platform:" + selected.ID + ":" + strconv.FormatUint(uint64(selected.Version), 10)})
	if err != nil {
		return err
	}
	a := agent.New(sender, os.Args[2])
	a.System = "LOCAL_RUNTIME_GUIDANCE"
	reply, err := a.Run(ctx, "hello", []agent.ToolDefinition{(tools.PlatformSkillTool{}).Definition()}, tools.NewDefaultRegistry())
	if err != nil {
		return err
	}
	if reply.Content != "answer" {
		return fmt.Errorf("gateway response mismatch: %q", reply.Content)
	}
	return nil
}
