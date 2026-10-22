package agent

// OCTO-FORK: preserve total tool latency while separating policy/queue wait from execution.
import "time"

const EventToolPhase EventKind = "tool_phase"

type ToolTiming struct {
	Phase       string `json:"phase"`
	Timestamp   int64  `json:"ts"`
	ApprovalMS  int64  `json:"approval_ms"`
	QueueMS     int64  `json:"queue_ms"`
	ExecutionMS int64  `json:"execution_ms"`
}

type toolClock struct {
	batchStart     time.Time
	approval       time.Duration
	executionStart time.Time
	block          ContentBlock
	handler        EventHandler
}

func (t *toolClock) emit(phase string) {
	if t.handler == nil {
		return
	}
	now := time.Now()
	waitEnd := now
	execution := time.Duration(0)
	if !t.executionStart.IsZero() {
		waitEnd = t.executionStart
		execution = now.Sub(t.executionStart)
	}
	wait := waitEnd.Sub(t.batchStart) - t.approval
	if wait < 0 {
		wait = 0
	}
	t.handler(AgentEvent{Kind: EventToolPhase, ToolID: t.block.ID, ToolName: t.block.Name,
		Timing: &ToolTiming{Phase: phase, Timestamp: now.UnixMilli(), ApprovalMS: t.approval.Milliseconds(), QueueMS: wait.Milliseconds(), ExecutionMS: execution.Milliseconds()}})
}

func (t *toolClock) start() { t.executionStart = time.Now(); t.emit("running") }
