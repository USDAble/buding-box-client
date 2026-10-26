package agent

import (
	"context"
	"testing"
	"time"
)

type timedGate struct{}

func (timedGate) Check(context.Context, string, map[string]any) (bool, string) {
	time.Sleep(30 * time.Millisecond)
	return true, ""
}

type timedExec struct{}

func (timedExec) Execute(context.Context, string, map[string]any) (ToolResult, error) {
	time.Sleep(10 * time.Millisecond)
	return ToolResult{Text: "ok"}, nil
}

func TestToolTimingSeparatesApprovalQueueAndExecution(t *testing.T) {
	for _, name := range []string{"read_file", "terminal"} {
		t.Run(name, func(t *testing.T) {
			var finished []ToolTiming
			var results int
			_, err := dispatchTools(context.Background(), timedExec{}, []ContentBlock{NewToolUseBlock("one", name, nil), NewToolUseBlock("two", name, nil)}, func(ev AgentEvent) {
				if ev.Kind == EventToolPhase && ev.Timing.Phase == "finished" {
					finished = append(finished, *ev.Timing)
				}
				if ev.Kind == EventToolDone {
					results++
				}
			}, timedGate{})
			if err != nil || results != 2 || len(finished) != 2 {
				t.Fatalf("dispatch: %v, results %d timing %d", err, results, len(finished))
			}
			for _, v := range finished {
				if v.ApprovalMS < 25 || v.QueueMS < 25 || v.ExecutionMS < 8 {
					t.Fatalf("missing stage: %+v", v)
				}
				if v.ExecutionMS >= v.ApprovalMS+v.QueueMS {
					t.Fatalf("execution includes approval: %+v", v)
				}
			}
		})
	}
}

func TestDeniedToolTimingHasNoExecution(t *testing.T) {
	var final *ToolTiming
	_, _ = dispatchTools(context.Background(), &countingExec{}, []ContentBlock{NewToolUseBlock("one", "grep", nil)}, func(ev AgentEvent) {
		if ev.Timing != nil && ev.Timing.Phase == "finished" {
			final = ev.Timing
		}
	}, denyOneGate{deny: "grep"})
	if final == nil || final.ExecutionMS != 0 {
		t.Fatalf("denied tool counted as execution: %+v", final)
	}
}
