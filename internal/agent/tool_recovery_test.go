package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type failedLookup struct{ calls int }

func (f *failedLookup) Execute(context.Context, string, map[string]any) (ToolResult, error) {
	f.calls++
	return ToolResult{}, errors.New("source unavailable")
}

func TestRecoveryHintUsesExistingRoundAndDoesNotStopWork(t *testing.T) {
	sender := &fakeToolSender{replies: []Reply{
		{StopReason: "tool_use", Blocks: []ContentBlock{NewToolUseBlock("a", "web_fetch", map[string]any{"url": "https://a.invalid"})}},
		{StopReason: "tool_use", Blocks: []ContentBlock{NewToolUseBlock("b", "web_fetch", map[string]any{"url": "https://b.invalid"})}},
		{StopReason: "end_turn", Content: "Delivered existing work; optional source unavailable."},
	}}
	a := New(sender, "model")
	executor := &failedLookup{}
	reply, err := a.RunStream(context.Background(), "finish the deliverable", []ToolDefinition{{Name: "web_fetch"}}, executor, nil)
	if err != nil || !strings.Contains(reply.Content, "Delivered") {
		t.Fatalf("work was stopped: %v %+v", err, reply)
	}
	if sender.calls != 3 || executor.calls != 2 {
		t.Fatalf("extra requests added: model=%d tools=%d", sender.calls, executor.calls)
	}
	var notices int
	for _, message := range sender.gotMsgs {
		for _, block := range message.Blocks {
			if strings.Contains(block.Result, toolRecoveryNotice) {
				notices++
			}
		}
	}
	if notices != 1 {
		t.Fatalf("recovery notices=%d", notices)
	}
}

func TestRecoveryDoesNotTreatMixedSuccessAsNoProgress(t *testing.T) {
	r := toolRecovery{}
	for range 2 {
		r.annotate([]ContentBlock{NewToolResultBlock("a", "failed", true)})
	}
	if !r.advised {
		t.Fatal("missing recovery advice")
	}
	mixed := []ContentBlock{NewToolResultBlock("a", "failed", true), NewToolResultBlock("b", "new useful information", false)}
	r.annotate(mixed)
	if r.failedRounds != 0 || r.advised {
		t.Fatal("useful work did not reset the streak")
	}
	firstFailure := []ContentBlock{NewToolResultBlock("c", "failed", true)}
	r.annotate(firstFailure)
	if strings.Contains(firstFailure[0].Result, toolRecoveryNotice) {
		t.Fatal("one failure triggered an unnecessary hint")
	}
}
