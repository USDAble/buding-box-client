// ai-guard 是便携版中采用失败关闭策略的 OpenAI 兼容网关命令。
//
// 开发边界：本命令完全位于 custom/portable 目录，仅通过配置的 API 基础地址
// 与上游 Octo 连接。严禁导入、修改或依赖上游内部业务实现。所有安全策略均以
// 外挂责任链方式在本文件中执行：
//
//	本地鉴权 -> 输入护栏 -> 提示词与工具策略 -> 上游模型
//	-> 工具与输出校验 -> 敏感数据脱敏
//
// 本命令仅使用 Go 标准库，使各平台产物保持为无需额外运行时依赖的小型原生程序。
package main

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// 网关强制固定在环回地址，便携版不得向局域网暴露模型代理或本地凭据。
	defaultListen = "127.0.0.1:18080"
	defaultToken  = "local-gateway-only"
	octoURL       = "http://127.0.0.1:18082"
	// 在转发或返回不可信载荷前执行硬限制，保护内存和 Token 预算。
	maxRequestBytes  = 8 << 20
	maxResponseBytes = 32 << 20
	maxOutputTokens  = 8192
)

var (
	// 内置规则覆盖凭据和常见个人数据；部署方可通过 U 盘策略文件补充业务敏感词。
	apiKeyPattern = regexp.MustCompile(`(?i)\b(sk-[a-z0-9_-]{16,})\b`)
	phonePattern  = regexp.MustCompile(`\b1[3-9][0-9]{9}\b`)
	// 词法检测是低成本的第一道拒绝层，用于减少明显的注入请求；它不能替代
	// 提示词隔离和工具授权，后两项仍由下方独立边界强制执行。
	injectionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|above)\s+(instructions?|prompts?)`),
		regexp.MustCompile(`(?i)reveal\s+(the\s+)?(system|developer)\s+(prompt|message)`),
		regexp.MustCompile(`(?i)(bypass|disable)\s+(the\s+)?(guardrails?|safety|policy)`),
		regexp.MustCompile(`(?i)you\s+are\s+now\s+(in\s+)?(developer|admin|unrestricted)\s+mode`),
	}
)

// settings 是进程启动时加载的完整策略快照。请求期间保持不变，避免策略被局部
// 加载或出现前后不一致。
type settings struct {
	listen         string
	upstreamURL    string
	upstreamAPIKey string
	upstreamModel  string
	localToken     string
	systemPrefix   string
	systemSuffix   string
	allowedTools   map[string]struct{}
	sensitiveTerms []string
}

// gateway 只负责外挂代理策略和 HTTP 传输，不引用 Octo 内部业务类型，以保持源码隔离。
type gateway struct {
	cfg    settings
	client *http.Client
}

func main() {
	// 配置采用失败关闭：缺少策略、模型或 HTTPS 端点时，在监听端口前终止进程。
	// 上游 API Key 可以为空，以便 U 盘启动阶段完全无交互；真正发送 AI 请求时
	// 仍会在下方拒绝无凭据请求，绝不把空凭据转发给外部服务。
	cfg, err := loadSettings()
	if err != nil {
		log.Fatal(err)
	}
	g := &gateway{
		cfg: cfg,
		client: &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		}},
	}

	// AI 路由由本进程直接处理，其余 UI/API/WS 请求转发给原版 Octo。
	// 反向代理会添加上游认可的转发标记，使环回访问也必须通过原版 access-key
	// 鉴权，从而在不修改上游认证代码的前提下保护便携版 UI 正常入口。
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", g.health)
	mux.HandleFunc("POST /v1/chat/completions", g.chatCompletions)
	mux.Handle("/", newOctoProxy())

	srv := &http.Server{
		Addr:              cfg.listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	log.Printf("ai-guard listening on %s; upstream host=%s; model=%s; upstream_key_configured=%t; allowed_tools=%d",
		cfg.listen, safeHost(cfg.upstreamURL), cfg.upstreamModel, cfg.upstreamAPIKey != "", len(cfg.allowedTools))
	log.Fatal(srv.ListenAndServe())
}

func newOctoProxy() *httputil.ReverseProxy {
	proxy, err := newReverseProxy(octoURL)
	if err != nil {
		// octoURL 是编译期常量，解析失败属于不可恢复的程序错误。
		panic(err)
	}
	return proxy
}

func newReverseProxy(rawURL string) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	direct := proxy.Director
	proxy.Director = func(r *http.Request) {
		direct(r)
		r.Host = target.Host
		// 上游只使用该标记收紧环回权限；它不会赋予请求额外权限。
		r.Header.Set("X-Octo-Forwarded", "portable-sidecar")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Printf("octo proxy failed: %v", err)
		writeError(w, http.StatusBadGateway, "octo unavailable")
	}
	return proxy, nil
}

func loadSettings() (settings, error) {
	cfg := settings{
		listen:         envOr("AI_GUARD_LISTEN", defaultListen),
		upstreamURL:    strings.TrimRight(os.Getenv("AI_GUARD_UPSTREAM_URL"), "/"),
		upstreamAPIKey: os.Getenv("AI_GUARD_UPSTREAM_API_KEY"),
		upstreamModel:  strings.TrimSpace(os.Getenv("AI_GUARD_UPSTREAM_MODEL")),
		localToken:     envOr("AI_GUARD_LOCAL_TOKEN", defaultToken),
	}
	if cfg.upstreamURL == "" || cfg.upstreamModel == "" {
		return settings{}, errors.New("AI_GUARD_UPSTREAM_URL and AI_GUARD_UPSTREAM_MODEL are required")
	}
	// 真实模型流量必须使用 HTTPS；HTTP 仅用于 Octo 与本地 Sidecar 之间的环回通信。
	if u, err := url.Parse(cfg.upstreamURL); err != nil || u.Scheme != "https" || u.Host == "" {
		return settings{}, errors.New("AI_GUARD_UPSTREAM_URL must be an absolute https URL")
	}
	// 禁止通过环境变量覆盖监听地址，防止扩大网络暴露范围。
	if cfg.listen != defaultListen {
		return settings{}, fmt.Errorf("AI_GUARD_LISTEN must remain %s in the portable build", defaultListen)
	}
	var err error
	if cfg.systemPrefix, err = readRequiredFile("AI_GUARD_SYSTEM_PREFIX_FILE"); err != nil {
		return settings{}, err
	}
	if cfg.systemSuffix, err = readRequiredFile("AI_GUARD_SYSTEM_SUFFIX_FILE"); err != nil {
		return settings{}, err
	}
	if cfg.allowedTools, err = readListFile(os.Getenv("AI_GUARD_ALLOWED_TOOLS_FILE")); err != nil {
		return settings{}, fmt.Errorf("allowed tools: %w", err)
	}
	if cfg.sensitiveTerms, err = readStringListFile(os.Getenv("AI_GUARD_SENSITIVE_TERMS_FILE")); err != nil {
		return settings{}, fmt.Errorf("sensitive terms: %w", err)
	}
	return cfg, nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func readRequiredFile(envName string) (string, error) {
	path := os.Getenv(envName)
	if path == "" {
		return "", fmt.Errorf("%s is required", envName)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", envName, err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", fmt.Errorf("%s is empty", envName)
	}
	return value, nil
}

func readListFile(path string) (map[string]struct{}, error) {
	items, err := readStringListFile(path)
	if err != nil {
		return nil, err
	}
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		result[item] = struct{}{}
	}
	return result, nil
}

func readStringListFile(path string) ([]string, error) {
	if path == "" {
		return nil, errors.New("list file path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// 支持空行和注释，使 U 盘策略文件便于维护，同时避免引入第二套配置解析器。
	var result []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			result = append(result, line)
		}
	}
	sort.Strings(result)
	return result, nil
}

func (g *gateway) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"status":"ok"}`)
}

func (g *gateway) chatCompletions(w http.ResponseWriter, r *http.Request) {
	// 本地令牌用于防止无关本机客户端误用；常量时间比较避免泄露部分令牌匹配信息。
	if !constantTokenEqual(bearerToken(r.Header.Get("Authorization")), g.cfg.localToken) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	// 启动阶段允许没有上游 Key，但 AI 请求必须失败关闭；这样桌面可以先打开，
	// 同时不会向外部模型服务发送空 Authorization，也不会把无凭据请求伪装成成功。
	if g.cfg.upstreamAPIKey == "" {
		writeError(w, http.StatusServiceUnavailable, "upstream credentials not configured")
		return
	}
	body, err := readLimited(r.Body, maxRequestBytes)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	// 调用前边界：拒绝非法输入、注入强制指令、替换客户端模型、限制输出并移除禁用工具。
	mutated, stream, err := g.guardRequest(body)
	if err != nil {
		log.Printf("blocked request: %v", err)
		writeError(w, http.StatusBadRequest, "request rejected by policy")
		return
	}

	target, err := upstreamEndpoint(g.cfg.upstreamURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "invalid upstream endpoint")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(mutated))
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not create upstream request")
		return
	}
	// 真实上游密钥只添加到本次出站请求，既不从上游应用接收，也不返回给上游应用。
	req.Header.Set("Authorization", "Bearer "+g.cfg.upstreamAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		log.Printf("upstream request failed: %v", err)
		writeError(w, http.StatusBadGateway, "upstream unavailable")
		return
	}
	defer resp.Body.Close()
	responseBody, err := readLimited(resp.Body, maxResponseBytes)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream response too large")
		return
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 模型服务错误可能回显凭据或个人数据，因此到达 UI 前同样必须经过脱敏边界。
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write([]byte(g.redactString(string(responseBody))))
		return
	}

	// 调用后边界：完整响应通过工具校验和输出脱敏前，不向 Octo 返回任何字节。
	// 此处有意缓冲 SSE，避免后续出现的不安全工具片段在拒绝前已经向外泄露。
	sanitized, contentType, err := g.guardResponse(responseBody, stream)
	if err != nil {
		log.Printf("blocked upstream response: %v", err)
		writeError(w, http.StatusBadGateway, "upstream response rejected by policy")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(sanitized)
}

func (g *gateway) guardRequest(body []byte) ([]byte, bool, error) {
	// 解码为通用 OpenAI 兼容结构，避免独立扩展与上游请求类型产生代码耦合。
	var request map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&request); err != nil {
		return nil, false, fmt.Errorf("invalid JSON: %w", err)
	}
	messages, ok := request["messages"].([]any)
	if !ok || len(messages) == 0 {
		return nil, false, errors.New("messages must be a non-empty array")
	}
	for _, message := range messages {
		if hasUntrustedPromptInjection(message) {
			return nil, false, errors.New("prompt injection signature")
		}
	}
	// 强制策略夹在调用方消息前后；网关会重建最终消息列表，客户端载荷无法移除该策略。
	request["messages"] = append([]any{
		map[string]any{"role": "system", "content": g.cfg.systemPrefix},
	}, append(messages, map[string]any{"role": "system", "content": g.cfg.systemSuffix})...)
	// 不信任客户端选择的模型；无论 Octo 或用户内容请求什么，只转发网关批准的模型。
	request["model"] = g.cfg.upstreamModel
	clampNumber(request, "max_tokens", maxOutputTokens)
	clampNumber(request, "max_completion_tokens", maxOutputTokens)
	g.filterTools(request)
	stream, _ := request["stream"].(bool)
	mutated, err := json.Marshal(request)
	return mutated, stream, err
}

func hasUntrustedPromptInjection(value any) bool {
	message, ok := value.(map[string]any)
	if !ok {
		return false
	}
	role, _ := message["role"].(string)
	// system 和 assistant 消息属于应用控制上下文。只扫描 user/tool 内容，避免防御性
	// 系统提示词仅因描述了禁止的注入语句而误拦截自身。
	if role != "user" && role != "tool" {
		return false
	}
	return hasPromptInjection(message["content"])
}

func hasPromptInjection(value any) bool {
	var texts []string
	collectStrings(value, &texts)
	for _, text := range texts {
		for _, pattern := range injectionPatterns {
			if pattern.MatchString(text) {
				return true
			}
		}
	}
	return false
}

func collectStrings(value any, out *[]string) {
	// 多模态 OpenAI 载荷可能在数组和 content 对象中嵌套文本；这里只检查文本内容字段，
	// 不扫描无关元数据。
	switch value := value.(type) {
	case string:
		*out = append(*out, value)
	case []any:
		for _, item := range value {
			collectStrings(item, out)
		}
	case map[string]any:
		for key, item := range value {
			if key == "content" || key == "text" {
				collectStrings(item, out)
			}
		}
	}
}

func clampNumber(request map[string]any, key string, maximum int64) {
	value, ok := request[key].(json.Number)
	if !ok {
		return
	}
	n, err := value.Int64()
	// 非法值、负数和超限值统一收敛到批准的上限。
	if err != nil || n < 1 || n > maximum {
		request[key] = maximum
	}
}

func (g *gateway) filterTools(request map[string]any) {
	// 工具声明本身就是能力授权；在模型选择工具前移除未批准声明，落实最小权限原则。
	tools, ok := request["tools"].([]any)
	if !ok {
		return
	}
	filtered := make([]any, 0, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		function, ok := tool["function"].(map[string]any)
		if !ok {
			continue
		}
		name, _ := function["name"].(string)
		if _, allowed := g.cfg.allowedTools[name]; allowed {
			filtered = append(filtered, tool)
		}
	}
	if len(filtered) == 0 {
		// 同时删除 tool_choice，防止调用方强制选择已从声明列表中移除的工具。
		delete(request, "tools")
		delete(request, "tool_choice")
		return
	}
	request["tools"] = filtered
	if choice, ok := request["tool_choice"].(map[string]any); ok {
		if fn, ok := choice["function"].(map[string]any); ok {
			name, _ := fn["name"].(string)
			if _, allowed := g.cfg.allowedTools[name]; !allowed {
				delete(request, "tool_choice")
			}
		}
	}
}

func (g *gateway) guardResponse(body []byte, stream bool) ([]byte, string, error) {
	if stream {
		if err := validateSSEToolCalls(body, g.cfg.allowedTools); err != nil {
			return nil, "", err
		}
		clean, err := g.redactSSE(body)
		return clean, "text/event-stream", err
	}
	var response any
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, "", fmt.Errorf("invalid response JSON: %w", err)
	}
	if err := validateJSONToolCalls(response, g.cfg.allowedTools); err != nil {
		return nil, "", err
	}
	g.redactValue(response)
	clean, err := json.Marshal(response)
	return clean, "application/json", err
}

type partialToolCall struct {
	name      string
	arguments strings.Builder
}

func validateSSEToolCalls(body []byte, allowed map[string]struct{}) error {
	// 工具名称和 JSON 参数可能分散在多个 SSE 事件中；必须先重组完整调用再校验，
	// 防止看似安全的前缀绕过策略。
	calls := map[string]*partialToolCall{}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("invalid SSE JSON: %w", err)
		}
		collectStreamToolCalls(event, calls)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	for id, call := range calls {
		// V1 校验函数白名单和 JSON 语法。启用任何写入、终端或数据库能力前，
		// 必须补充逐工具 JSON Schema 以及 Shell/SQL AST 校验。
		if _, ok := allowed[call.name]; !ok {
			return fmt.Errorf("tool call %s uses disallowed tool %q", id, call.name)
		}
		if call.arguments.Len() > 0 && !json.Valid([]byte(call.arguments.String())) {
			return fmt.Errorf("tool call %s has invalid JSON arguments", id)
		}
	}
	return nil
}

func collectStreamToolCalls(event map[string]any, calls map[string]*partialToolCall) {
	choices, _ := event["choices"].([]any)
	for _, rawChoice := range choices {
		choice, _ := rawChoice.(map[string]any)
		delta, _ := choice["delta"].(map[string]any)
		toolCalls, _ := delta["tool_calls"].([]any)
		for _, rawCall := range toolCalls {
			call, _ := rawCall.(map[string]any)
			id := ""
			// 优先使用稳定的 choice 内部索引，因为服务方通常只在流式工具调用的首片段提供 ID。
			if index, exists := call["index"]; exists {
				id = fmt.Sprint(index)
			}
			if id == "" {
				id, _ = call["id"].(string)
			}
			partial := calls[id]
			if partial == nil {
				partial = &partialToolCall{}
				calls[id] = partial
			}
			function, _ := call["function"].(map[string]any)
			if name, _ := function["name"].(string); name != "" {
				partial.name = name
			}
			if arguments, _ := function["arguments"].(string); arguments != "" {
				partial.arguments.WriteString(arguments)
			}
		}
	}
}

func validateJSONToolCalls(value any, allowed map[string]struct{}) error {
	// 非流式响应与 SSE 使用同一套失败关闭工具边界。
	root, _ := value.(map[string]any)
	choices, _ := root["choices"].([]any)
	for _, rawChoice := range choices {
		choice, _ := rawChoice.(map[string]any)
		message, _ := choice["message"].(map[string]any)
		calls, _ := message["tool_calls"].([]any)
		for _, rawCall := range calls {
			call, _ := rawCall.(map[string]any)
			function, _ := call["function"].(map[string]any)
			name, _ := function["name"].(string)
			if _, ok := allowed[name]; !ok {
				return fmt.Errorf("disallowed tool call %q", name)
			}
			arguments, _ := function["arguments"].(string)
			if !json.Valid([]byte(arguments)) {
				return fmt.Errorf("tool %q has invalid JSON arguments", name)
			}
		}
	}
	return nil
}

func (g *gateway) redactSSE(body []byte) ([]byte, error) {
	// 逐个解析并重新编码 data 事件，使脱敏作用于结构化文本，同时不破坏 SSE 帧和
	// 结束标记 [DONE]。
	var out bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data != "" && data != "[DONE]" {
				var event any
				if err := json.Unmarshal([]byte(data), &event); err != nil {
					return nil, err
				}
				g.redactValue(event)
				clean, err := json.Marshal(event)
				if err != nil {
					return nil, err
				}
				line = "data: " + string(clean)
			}
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.Bytes(), scanner.Err()
}

func (g *gateway) redactValue(value any) {
	// 对每个字符串叶节点执行脱敏，包括嵌套内容和工具参数，防止其他响应结构绕过
	// 输出治理边界。
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			g.redactValue(item)
		}
	case map[string]any:
		for key, item := range value {
			if text, ok := item.(string); ok {
				value[key] = g.redactString(text)
			} else {
				g.redactValue(item)
			}
		}
	}
}

func (g *gateway) redactString(value string) string {
	value = apiKeyPattern.ReplaceAllString(value, "[REDACTED_API_KEY]")
	value = phonePattern.ReplaceAllString(value, "[REDACTED_PHONE]")
	for _, term := range g.cfg.sensitiveTerms {
		value = strings.ReplaceAll(value, term, "[REDACTED_TERM]")
	}
	return value
}

func upstreamEndpoint(base string) (string, error) {
	// 接受常见的 OpenAI 兼容基础地址形式，并统一生成固定的 chat-completions 地址；
	// loadSettings 已提前强制要求 HTTPS。
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(u.Path, "/")
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
	case strings.HasSuffix(path, "/v1"):
		u.Path = path + "/chat/completions"
	default:
		u.Path = path + "/v1/chat/completions"
	}
	return u.String(), nil
}

func readLimited(reader io.Reader, maximum int64) ([]byte, error) {
	// 额外读取一个字节，以区分刚好达到限制的载荷和被截断的超限载荷，同时避免无限分配。
	data, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("payload exceeds %d bytes", maximum)
	}
	return data, nil
}

func bearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

func constantTokenEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func safeHost(raw string) string {
	// 日志只记录主机名，严禁记录 URL 用户信息、查询参数、路径或上游 API Key。
	u, err := url.Parse(raw)
	if err != nil {
		return "invalid"
	}
	return u.Host
}

func writeError(w http.ResponseWriter, status int, message string) {
	// 返回 OpenAI 兼容错误并禁止缓存。调用方获得稳定的策略结果，详细拒绝原因仅保留在
	// U 盘本地日志中。
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": message, "type": "portable_gateway_error"},
	})
}
