package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
	"github.com/open-octo/octo-agent/internal/config"
)

// maskingSender is a no-network sender whose visible text is chosen by the
// test. It deliberately implements only the two interfaces a plain turn needs.
type maskingSender struct {
	deltas []string
	reply  agent.Reply
}

func (s *maskingSender) SendMessages(context.Context, string, string, []agent.Message, int) (agent.Reply, error) {
	return s.reply, nil
}

func (s *maskingSender) StreamMessages(_ context.Context, _, _ string, _ []agent.Message, _ int, onChunk func(string), _ func(string)) (agent.Reply, error) {
	for _, d := range s.deltas {
		if onChunk != nil {
			onChunk(d)
		}
	}
	return s.reply, nil
}

// dataRoot points the data root at a fresh directory. The dictionary lives
// under it (data/sensitive-words.txt), so this decides which file the engine
// reads.
func dataRoot(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// OCTO-FORK: 数据根：`~/.octo` → `<exe dir>/data`（硬规则 1）
	t.Setenv("OCTO_DATA_ROOT", tmp)
	t.Setenv("USERPROFILE", tmp)
	return tmp
}

// newFilteredServer is the minimal turn-capable server the other turn nails
// build, plus the engine the production constructor installs: the engine is the
// subject here, so it must not be the thing under test.
func newFilteredServer(t *testing.T, sender agent.Sender) *Server {
	t.Helper()
	srv := mustServer(t, Config{Addr: "127.0.0.1:0", Tools: false})
	srv.sensitiveEngine = NewSensitiveEngine()
	srv.sender = sender
	srv.initWS()
	srv.turnRunning = make(map[string]bool)
	srv.steerQueues = make(map[string][]queuedTurn)
	srv.sessionAgents = make(map[string]*agent.Agent)
	return srv
}

// runTurn drives one turn and returns the text the browser saw streamed, the
// text of the persisted reply, and the session's path on disk.
//
// The text is read back TWICE on purpose — from the file's bytes and from a
// fresh Load — because "落盘即打码" is a claim about the bytes. A save that
// wrote the raw word and a Load that masked it would satisfy the struct
// assertion alone.
func runTurn(t *testing.T, srv *Server, sess *agent.Session) (onScreen, stored, rawFile string) {
	t.Helper()
	conn := &wsConn{hub: srv.wsHub, send: make(chan []byte, 256), subscribed: map[string]struct{}{}}
	srv.wsHub.subscribe(conn, sess.ID)

	// doAgentTurn runs the turn to completion before returning (the crash-reminder
	// nails rely on the same), so the broadcast buffer and the session file are
	// both final below.
	srv.doAgentTurn(sess, "你好", nil, nil)

	var parts []string
	for _, ev := range drainConn(t, conn) {
		if ev["type"] != "text_delta" {
			continue
		}
		if s, ok := ev["text"].(string); ok {
			parts = append(parts, s)
		}
	}

	reloaded, err := agent.LoadSession(sess.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	p, err := sess.SavePath()
	if err != nil {
		t.Fatalf("SavePath: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	return strings.Join(parts, ""), lastAssistantText(t, reloaded), string(b)
}

// lastAssistantText returns the text of the last assistant message.
func lastAssistantText(t *testing.T, sess *agent.Session) string {
	t.Helper()
	for i := len(sess.Messages) - 1; i >= 0; i-- {
		m := sess.Messages[i]
		if m.Role != agent.RoleAssistant {
			continue
		}
		if m.Content != "" {
			return m.Content
		}
		for _, b := range m.Blocks {
			if b.Type == "text" {
				return b.Text
			}
		}
	}
	t.Fatal("no assistant message was persisted")
	return ""
}

func savedSession(t *testing.T, title string) *agent.Session {
	t.Helper()
	sess := agent.NewSession("stub-model", "")
	sess.Title = title
	if err := sess.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	return sess
}

// TestASensitiveWordIsMaskedOnScreenAndOnDisk is the criterion G2 turned into a
// nail: the mask is applied before the agent stores the reply, so a restart
// re-reads `***` instead of the word. The archived implementation guaranteed
// only the screening half.
//
// The word arrives split across two deltas — how a real stream splits it at
// arbitrary boundaries. A single-chunk test would pass on a wiring that only
// filters complete replies.
func TestASensitiveWordIsMaskedOnScreenAndOnDisk(t *testing.T) {
	dataRoot(t)
	srv := newFilteredServer(t, &maskingSender{
		deltas: []string{"您要的发", "票已开好"},
		reply:  agent.Reply{Content: "您要的发票已开好"},
	})
	sess := savedSession(t, "敏感词")

	onScreen, stored, rawFile := runTurn(t, srv, sess)

	if onScreen != "您要的***已开好" {
		t.Fatalf("streamed to the browser = %q, want 您要的***已开好", onScreen)
	}
	if stored != "您要的***已开好" {
		t.Fatalf("reloaded reply = %q, want the masked text", stored)
	}
	if strings.Contains(rawFile, "发票") {
		t.Fatalf("the session file on disk holds the word in plain text: %s", rawFile)
	}
}

// TestAPlainReplyIsStoredByteForByte is the counter-nail: with no match the
// filter must be a no-op, so a "顺手的规范化/截断" cannot hide behind it.
func TestAPlainReplyIsStoredByteForByte(t *testing.T) {
	dataRoot(t)
	const reply = "hello 世界\t你好\n"
	srv := newFilteredServer(t, &maskingSender{
		deltas: []string{"hello 世", "界\t你好\n"},
		reply:  agent.Reply{Content: reply},
	})
	sess := savedSession(t, "无命中")

	onScreen, stored, _ := runTurn(t, srv, sess)

	if onScreen != reply {
		t.Fatalf("streamed = %q, want %q", onScreen, reply)
	}
	if stored != reply {
		t.Fatalf("persisted = %q, want %q", stored, reply)
	}
}

// TestTheUsersOwnDictionaryIsTheOneThatIsRead: the engine's own tests pass a
// path themselves, so nothing there can see whether the server resolved the path
// the requirement names (data/sensitive-words.txt). Same class of blind spot as
// a wrong JSON tag — a wiring fact no engine test can fail on.
func TestTheUsersOwnDictionaryIsTheOneThatIsRead(t *testing.T) {
	root := dataRoot(t)
	word := "本店特供词"
	if err := os.WriteFile(filepath.Join(root, "sensitive-words.txt"), []byte(word+"\n"), 0o600); err != nil {
		t.Fatalf("write user dictionary: %v", err)
	}
	srv := newFilteredServer(t, &maskingSender{
		deltas: []string{"这是", word},
		reply:  agent.Reply{Content: "这是" + word},
	})
	sess := savedSession(t, "用户词表")

	onScreen, stored, _ := runTurn(t, srv, sess)

	if onScreen != "这是***" || stored != "这是***" {
		t.Fatalf("screen = %q, disk = %q — the user dictionary was not the one read", onScreen, stored)
	}
}

// TestAMissingDictionaryIsNotCreated: "no file" means "use the built-in list",
// never "materialise the default" (开发规范 §3.9.1). The first run must not write
// a user-editable file, and the built-in words must still be enforced.
func TestAMissingDictionaryIsNotCreated(t *testing.T) {
	root := dataRoot(t)
	srv := newFilteredServer(t, &maskingSender{reply: agent.Reply{Content: "赌博"}})
	sess := savedSession(t, "无词表")

	_, stored, _ := runTurn(t, srv, sess)

	if stored != "***" {
		t.Fatalf("persisted = %q, want the built-in word masked", stored)
	}
	if _, err := os.Stat(filepath.Join(root, "sensitive-words.txt")); err == nil {
		t.Fatal("a dictionary file was created — §3.9.1 forbids writing a user-editable file")
	}
}

// TestTheTurnAndChannelSeamsBothCarryTheFilter asserts the two reachable seams
// behaviourally, so TestEveryAgentConstructionCarriesTheFilter is not the only
// evidence that the filter is wired.
func TestTheTurnAndChannelSeamsBothCarryTheFilter(t *testing.T) {
	dataRoot(t)
	srv := newFilteredServer(t, &maskingSender{reply: agent.Reply{Content: "发票"}})

	turnAgent := srv.buildAgent(agent.NewSession("stub-model", ""))
	reply, err := turnAgent.Sender.SendMessages(context.Background(), "m", "s", nil, 0)
	if err != nil {
		t.Fatalf("turn sender: %v", err)
	}
	if reply.Content != "***" {
		t.Fatalf("turn-path reply = %q, want the masked text", reply.Content)
	}

	channelAgent := srv.buildChannelAgent(nil)
	reply, err = channelAgent.Sender.SendMessages(context.Background(), "m", "s", nil, 0)
	if err != nil {
		t.Fatalf("channel sender: %v", err)
	}
	if reply.Content != "***" {
		t.Fatalf("IM-path reply = %q, want the masked text", reply.Content)
	}
}

// TestEveryAgentConstructionCarriesTheFilter is the structural half of the
// wiring. Three seams build an agent (buildAgent, the sub-agent template, the IM
// channel); the sub-agent template is not reachable without a configured default
// sender, so it cannot be asserted behaviourally. What this covers is a FOURTH
// seam added later — no behavioural test would notice it until a user saw
// unmasked text.
func TestEveryAgentConstructionCarriesTheFilter(t *testing.T) {
	entries, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	found := 0
	for _, name := range entries {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if !strings.Contains(line, "agent.New(") {
				continue
			}
			found++
			if !strings.Contains(line, "wrapSensitive(") {
				t.Errorf("%s:%d builds an agent with no filter: %s", name, i+1, strings.TrimSpace(line))
			}
		}
	}
	if found < 3 {
		t.Fatalf("found only %d agent construction site(s) — this scan no longer sees them, so it guards nothing", found)
	}
}

// TestTheProductionConstructorBuildsTheEngine: the constructor assigning the
// field is what makes the filter present in a shipped build. The turn nails
// above install the engine themselves and would stay green without it.
func TestTheProductionConstructorBuildsTheEngine(t *testing.T) {
	dataRoot(t)
	t.Setenv("OPENAI_API_KEY", "")
	srv, err := New(Config{Addr: "127.0.0.1:0", NoChannel: true, NoMemory: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.sensitiveEngine == nil {
		t.Fatal("New() left sensitiveEngine nil — a shipped server would filter nothing")
	}
	// A fresh data root has no dictionary file, so the engine must be serving the
	// built-in list. FileUnreadable() is what distinguishes "degraded to built-in"
	// from "read the user's file"; a nil check alone cannot.
	if !srv.sensitiveEngine.FileUnreadable() {
		t.Fatal("with no dictionary file present the engine should report the built-in fallback")
	}
}

// TestTheStreamedAndPersistedTextsAgree closes the loop on what the browser
// renders: a refresh replays history, so if disk held the raw word the user
// would watch the mask turn back into the word.
func TestTheStreamedAndPersistedTextsAgree(t *testing.T) {
	dataRoot(t)
	srv := newFilteredServer(t, &maskingSender{
		deltas: []string{"发", "票"},
		reply:  agent.Reply{Content: "发票"},
	})
	sess := savedSession(t, "一致")

	onScreen, stored, _ := runTurn(t, srv, sess)

	if onScreen != "***" {
		t.Fatalf("streamed = %q, want ***", onScreen)
	}
	if onScreen != stored {
		t.Fatalf("browser saw %q but disk holds %q", onScreen, stored)
	}
}

// TestAConfiguredLiteSendersTextIsMaskedToo covers the second model-text path:
// session titles and compaction summaries are produced by the lite sender and
// are user-visible (the session list, the folded turn). It is only built when
// config.yml names a lite model, which is why no turn nail can see it.
//
// The endpoint points at a local server, so the assertion runs through the real
// app.NewSender + openai provider rather than a stub — the same "no criterion
// could see the wiring" lesson as the JSON tag: the wrap has to be proven on the
// object the turn actually sends with.
func TestAConfiguredLiteSendersTextIsMaskedToo(t *testing.T) {
	setTestHome(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"您要的发票已开好"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()

	seedModels(t, config.Config{
		Endpoints: []config.Endpoint{{
			ID:       "ep-lite",
			Provider: "custom",
			BaseURL:  upstream.URL,
			Protocol: "openai",
			APIKey:   "sk-lite",
			Models:   []config.EndpointModel{{Model: "lite-model"}},
		}},
		Default: "ep-lite::lite-model",
		Lite:    "ep-lite::lite-model",
	})
	srv := newFilteredServer(t, &maskingSender{reply: agent.Reply{Content: "unused"}})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	lite, model := srv.liteSenderFromConfig(cfg)
	if lite == nil || model == "" {
		t.Fatal("no lite sender was built — the fixture no longer exercises this path")
	}

	reply, err := lite.SendMessages(context.Background(), model, "system",
		[]agent.Message{agent.NewUserMessage("你好")}, 32)
	if err != nil {
		t.Fatalf("lite send: %v", err)
	}
	if reply.Content != "您要的***已开好" {
		t.Fatalf("lite reply = %q, want the masked text", reply.Content)
	}
}
