package agent

// toolRecoveryNotice is a once-per-streak hint carried in an existing failed
// tool result. It never creates another provider round or stops useful work.
const toolRecoveryNotice = "Repeated tool failures: check the shared cause before another attempt. Reuse facts already obtained; do not repeat a failed approach under unchanged conditions. If this step is optional, preserve and deliver completed work with the unmet check stated. If it is required, explain the concrete blocker rather than inventing a result."

type toolRecovery struct {
	failedRounds int
	advised      bool
}

func (r *toolRecovery) annotate(results []ContentBlock) {
	lastFailure := -1
	for i, result := range results {
		if result.Type != "tool_result" {
			continue
		}
		if !result.IsError {
			*r = toolRecovery{}
			return
		}
		lastFailure = i
	}
	if lastFailure < 0 {
		return
	}
	r.failedRounds++
	if r.failedRounds >= 2 && !r.advised {
		results[lastFailure].Result += "\n\n" + toolRecoveryNotice
		r.advised = true
	}
}
