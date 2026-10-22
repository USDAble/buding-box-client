package server

// OCTO-FORK: expose dispatch-owned timings without logging tool input or output.
import (
	"github.com/open-octo/octo-agent/internal/agent"
	"log/slog"
)

// Called with the stream writer's mutex held, just like other replayable events.
func (w *wsStreamWriter) handleToolPhase(ev agent.AgentEvent) {
	t := ev.Timing
	if t == nil {
		return
	}
	event := map[string]any{"type": "tool_phase", "session_id": w.sessionID,
		"tool_id": ev.ToolID, "name": ev.ToolName, "phase": t.Phase, "ts": t.Timestamp,
		"approval_ms": t.ApprovalMS, "queue_ms": t.QueueMS, "execution_ms": t.ExecutionMS}
	w.hub.broadcast(w.sessionID, event)
	w.bufferTurnEvent(event)
	if t.Phase == "finished" {
		slog.Info("tool dispatch timing", "session_id", w.sessionID, "tool_id", ev.ToolID,
			"tool", ev.ToolName, "approval_ms", t.ApprovalMS, "queue_ms", t.QueueMS, "execution_ms", t.ExecutionMS)
	}
}
