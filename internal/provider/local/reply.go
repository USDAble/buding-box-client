//go:build !product_production

package local

import (
	"regexp"
	"strings"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/provider"
)

// replyText returns the deterministic fake reply for one request. The design
// (§3.3) requires the reply text to be hit by output sensitive-word filtering
// so a demo visibly shows "***"; the generic and invoice branches both embed
// the built-in word 发票 / invoice for that. The phone branch echoes the
// number back so P10's PII masking can be eyeballed.
func replyText(req provider.Request) string {
	user := lastUserText(req.Messages)

	if hasPhone(user) {
		return "我看到你提供的号码是 " + user + "，已收到。"
	}
	if containsAny(user, "发票", "invoice", "fapiao") {
		return "这是一段包含发票与 gambling 的演示说明文本，用于验证输出敏感词过滤是否生效。"
	}
	return "这是本地演示模型的固定回复。你刚才说的是：" + user + "。" +
		"\n（演示：本条包含示例敏感词 发票 / invoice，用于验证输出过滤。）"
}

// lastUserText returns the most recent user-turn prose, walking the message
// list from the end so multi-turn requests resolve to the latest prompt.
func lastUserText(msgs []agent.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == agent.RoleUser {
			if t := messageText(msgs[i]); t != "" {
				return t
			}
		}
	}
	return ""
}

// phoneRe matches an 11-digit mainland-China mobile number, the P10 PII shape.
// A loose match is fine here: the fake model only needs to detect "this looks
// like a number" to switch to the echo branch.
var phoneRe = regexp.MustCompile(`1[3-9]\d{9}`)

func hasPhone(s string) bool { return phoneRe.MatchString(s) }

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
