package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/open-octo/octo-agent/internal/productclient/clienttest"
)

// The optional real-model upstream.
//
// WHY THIS EXISTS. The stand-in's completions route answers with canned text, so
// every walkthrough so far demonstrated the wiring and nothing else: the model
// was a fixture, and "does the product really talk to a model" stayed untestable
// until the Sandbox address arrives (需求基线 A1 规则 7). Pointing the gateway at
// a real provider is possible today because the gateway speaks plain OpenAI
// (internal/productruntime/gateway.go: protocol "openai"), but two facts stand in
// the way and both are handled here rather than in the product:
//
//  1. THE CLIENT AUTHENTICATES WITH THE PLATFORM TOKEN. GatewayEndpoint passes
//     the platform access token as the provider API key, because that is what the
//     real gateway expects. A real provider expects its own key, so the proxy
//     swaps the header. Forwarding it unchanged would hand a third party a live
//     session token - a worse version of the leak V-35 was about (需求基线 B4).
//  2. THE CATALOG IDS ARE FIXTURES. The client forwards the bare catalog id
//     ("buding-privacy-1", L-C3a), which no real provider knows. -model rewrites
//     it. Doing this with a mapping table in the product would be inventing a
//     contract; a flag on a developer tool is not a contract.
//
// SCOPE (开发规范 §3.10): developer, local, run-time only. Off by default, so an
// unconfigured run behaves exactly as before and dials nothing. It lives in the
// dev binary rather than in clienttest because clienttest mirrors the platform
// contract and every fixture there must stay deterministic - a fixture that
// sometimes reached a real model would break the nails that assert its replies.
// (The library gained one accessor, Authorised, so the token question stays with
// the code that owns the token table.)
//
// IT IS NOT A PRODUCTION PATH. Nothing in packaging/ builds this binary, and no
// product code imports this package.

// gatewayFlags is the -upstream side of the command line.
type gatewayFlags struct {
	// baseURL is the provider's base. Both "https://api.deepseek.com" and
	// ".../v1" are accepted, because the provider client this mirrors tolerates
	// both and an operator copying either shape must not get /v1/v1.
	baseURL string
	// apiKey is the provider's key. It replaces the platform token on the way out
	// and is never logged.
	apiKey string
	// model rewrites the outgoing model id. Empty passes the client's own value
	// through, which only a provider that ignores the field will accept.
	model string
}

// enabled reports whether the proxy should intercept turns at all.
func (f gatewayFlags) enabled() bool { return strings.TrimSpace(f.baseURL) != "" }

// completionsURL joins a base with the completions path.
//
// It trims a trailing slash and a trailing /v1 before appending /v1/chat/
// completions: the same normalisation internal/provider/openai performs, so the
// two agree on what a base means instead of each having its own idea (the
// confusion that produced V-37).
func completionsURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	base = strings.TrimSuffix(base, "/v1")
	return base + "/v1/chat/completions"
}

// validateFlags refuses the one combination that would make a walkthrough
// demonstrate something the operator did not ask for. -tool makes the fixture
// invent a tool call; -upstream hands the turn to a model that decides for
// itself. Honouring either silently would leave the other's absence unexplained.
func validateFlags(tool, upstream string) error {
	if strings.TrimSpace(tool) != "" && strings.TrimSpace(upstream) != "" {
		return fmt.Errorf("-tool and -upstream cannot be combined: -tool makes the fixture invent a tool call, while -upstream lets a real model decide; drop one of them")
	}
	return nil
}

// describeGateway is the banner's account of where a turn will go.
//
// Stated on every run because the two modes are indistinguishable from the
// outside - both answer with a stream - and "the model is a fixture" versus "the
// model is real" is the single most important thing to know while reading the
// reply (the same reason printFixtures states the tool switch).
func describeGateway(f gatewayFlags) string {
	if !f.enabled() {
		return "  gateway      OFF - the canned stand-in reply (no network)"
	}
	lines := fmt.Sprintf("  gateway      REAL upstream %s\n               the platform token is replaced with the provider key", f.baseURL)
	if strings.TrimSpace(f.model) == "" {
		lines += "\n               model ids pass through unchanged - a real provider will reject `buding-*`; pass -model=<real id>"
	} else {
		lines += fmt.Sprintf("\n               model rewrite: %s -> %s", "<catalog id>", f.model)
	}
	return lines
}

// withUpstreamGateway forwards model turns to a real provider when configured.
//
// ORDER IS THE POINT. The token check runs BEFORE the provider is dialled, and
// it asks the fixture rather than inspecting the bearer: without it this process
// would be an open proxy on loopback that spends the operator's provider
// credits, reachable by anything on the machine.
//
// The body is rewritten through map[string]any rather than a typed struct so
// every field the client sends - tools, reasoning_effort, stream_options - is
// forwarded untouched. A typed struct would silently drop the next field the
// client learns to send, which is exactly the failure mode CLAUDE.md's
// "unknown fields are ignored" note is about.
func withUpstreamGateway(next http.Handler, stub *clienttest.Server, cfg gatewayFlags) http.Handler {
	if !cfg.enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			next.ServeHTTP(w, r)
			return
		}
		if !stub.Authorised(r) {
			// The stand-in's own refusal, so the operator sees the same 401 body
			// in both modes and the log line's status stays meaningful.
			next.ServeHTTP(w, r)
			return
		}
		serveFromUpstream(w, r, cfg)
	})
}

// serveFromUpstream performs the forward. Errors are reported in the client's
// own envelope so the desktop UI renders them like any other platform failure
// rather than as a proxy-shaped surprise.
func serveFromUpstream(w http.ResponseWriter, r *http.Request, cfg gatewayFlags) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeProxyError(w, http.StatusBadRequest, "the request body could not be read")
		return
	}
	body := map[string]any{}
	if err := json.Unmarshal(raw, &body); err != nil {
		writeProxyError(w, http.StatusBadRequest, "the request body is not JSON")
		return
	}
	if strings.TrimSpace(cfg.model) != "" {
		body["model"] = cfg.model
	}
	out, err := json.Marshal(body)
	if err != nil {
		writeProxyError(w, http.StatusInternalServerError, "the request could not be rewritten")
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, completionsURL(cfg.baseURL), bytes.NewReader(out))
	if err != nil {
		writeProxyError(w, http.StatusInternalServerError, "the upstream request could not be built")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	// The provider's key, never the platform's token.
	req.Header.Set("Authorization", "Bearer "+cfg.apiKey)
	if accept := r.Header.Get("Accept"); accept != "" {
		req.Header.Set("Accept", accept)
	} else {
		req.Header.Set("Accept", "text/event-stream")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// The address is named because a typo and an outage are the two ways
		// this fails and only one of them is fixed by waiting.
		writeProxyError(w, http.StatusBadGateway, fmt.Sprintf("the upstream %s could not be reached: %v", completionsURL(cfg.baseURL), err))
		return
	}
	defer resp.Body.Close()

	// The provider's status and type pass through: a 400 that names the model is
	// the answer to "why is nothing happening", and replacing it with a generic
	// message would hide the one line the operator needs.
	for _, h := range []string{"Content-Type", "Cache-Control"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	// Streamed chunk by chunk with a flush per read, so the reply appears as the
	// provider produces it. Buffering here would make a working streaming client
	// look like a non-streaming one - the aggregation the provider does is one of
	// the things a hand walkthrough exists to exercise.
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

// writeProxyError answers in the envelope internal/server renders, so a proxy
// failure looks like a platform failure to the UI.
func writeProxyError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    "upstream_unavailable",
		"message": message,
	})
}
