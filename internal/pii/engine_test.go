package pii

import (
	"encoding/json"
	"encoding/pem"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestTransformBuiltInRules(t *testing.T) {
	privateKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte{1, 2, 3, 4}}))
	tests := []struct {
		name        string
		input       string
		want        string
		wantMatches []MatchSummary
	}{
		{
			name:        "resident ID",
			input:       "证件 11010519491231002X。",
			want:        "证件 <身份证号>。",
			wantMatches: []MatchSummary{{Category: "cn_resident_id", Count: 1}},
		},
		{
			name:        "mobile with separators",
			input:       "电话：138 0013 8000",
			want:        "电话：<手机号>",
			wantMatches: []MatchSummary{{Category: "cn_mobile", Count: 1}},
		},
		{
			name:        "email",
			input:       "联系 alice.smith+tag@example.co.uk，谢谢",
			want:        "联系 <邮箱>，谢谢",
			wantMatches: []MatchSummary{{Category: "email", Count: 1}},
		},
		{
			name:        "bank card",
			input:       "卡号 4111 1111 1111 1111",
			want:        "卡号 <银行卡号>",
			wantMatches: []MatchSummary{{Category: "bank_card", Count: 1}},
		},
		{
			name:        "VIN",
			input:       "VIN: 1M8GDM9AXKP042788",
			want:        "VIN: <VIN>",
			wantMatches: []MatchSummary{{Category: "vin", Count: 1}},
		},
		{
			name:        "IPv4",
			input:       "服务地址 192.168.1.10。",
			want:        "服务地址 <IP地址>。",
			wantMatches: []MatchSummary{{Category: "ip_address", Count: 1}},
		},
		{
			name:        "IPv6 after label",
			input:       "IP:2001:db8::1。",
			want:        "IP:<IP地址>。",
			wantMatches: []MatchSummary{{Category: "ip_address", Count: 1}},
		},
		{
			name:        "PEM private key",
			input:       "配置如下：\n" + privateKey + "结束",
			want:        "配置如下：\n<私钥>结束",
			wantMatches: []MatchSummary{{Category: "pem_private_key", Count: 1}},
		},
		{
			name:        "explicit secret",
			input:       "key=sk-abcdefghijklmnop12345678",
			want:        "key=<访问凭据>",
			wantMatches: []MatchSummary{{Category: "secret_token", Count: 1}},
		},
		{
			name:        "contextual secret",
			input:       "api_key = abcdefghijklmnop12345678",
			want:        "api_key = <访问凭据>",
			wantMatches: []MatchSummary{{Category: "secret_token", Count: 1}},
		},
		{
			name:        "OTP",
			input:       "验证码是 123456，请勿转发",
			want:        "验证码是 <验证码>，请勿转发",
			wantMatches: []MatchSummary{{Category: "sms_otp", Count: 1}},
		},
	}

	engine := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.Transform(tt.input)
			if err != nil {
				t.Fatalf("Transform() error = %v", err)
			}
			if got.Masked != tt.want {
				t.Fatal("masked output mismatch")
			}
			if !reflect.DeepEqual(got.Matches, tt.wantMatches) {
				t.Fatalf("Matches = %#v, want %#v", got.Matches, tt.wantMatches)
			}
		})
	}
}

func TestTransformRejectsLookalikes(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "resident ID checksum", input: "错误身份证 110105194912310021"},
		{name: "resident ID date", input: "非法日期 11010519490231002X"},
		{name: "mobile embedded in longer number", input: "更长号码 91380013800138000"},
		{name: "email placeholder", input: "邮箱占位 a@b"},
		{name: "email consecutive dots", input: "连续点 a..b@example.com"},
		{name: "email domain underscore", input: "域名下划线 user@example_test.com"},
		{name: "bank card checksum", input: "错误卡号 4111 1111 1111 1112"},
		{name: "bank card repeated digits", input: "全同卡号 1111 1111 1111 1111"},
		{name: "VIN checksum", input: "错误 VIN 1M8GDM8AXKP042788"},
		{name: "VIN forbidden letter", input: "禁用字符 1M8GDM9AOKP042788"},
		{name: "version label", input: "版本 version 1.2.3.4"},
		{name: "short version label", input: "版本 v1.2.3.4"},
		{name: "invalid IP", input: "非法 IP 999.1.1.1"},
		{name: "IP embedded in longer dotted value", input: "更长 IP 1.2.3.4.5"},
		{name: "unlabelled commit hash", input: "提交哈希 0123456789abcdef0123456789abcdef01234567"},
		{name: "OTP without context", input: "无上下文 123456"},
		{name: "year", input: "年份 2026年"},
		{name: "amount", input: "金额 1234 元"},
		{name: "port", input: "端口 8080"},
		{name: "incomplete private key", input: "-----BEGIN PRIVATE KEY-----\nAQIDBA==\n"},
		{name: "public key", input: "-----BEGIN PUBLIC KEY-----\nAQIDBA==\n-----END PUBLIC KEY-----\n"},
	}

	engine := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.Transform(tt.input)
			if err != nil {
				t.Fatalf("Transform() error = %v", err)
			}
			if got.Masked != tt.input || len(got.Matches) != 0 {
				t.Fatalf("lookalike was unexpectedly masked as %#v", got.Matches)
			}
		})
	}
}

func TestTransformFullWidthInput(t *testing.T) {
	input := strings.Join([]string{
		"身份证１１０１０５１９４９１２３１００２Ｘ",
		"电话１３８－００１３－８０００",
		"邮箱ａｌｉｃｅ＠ｅｘａｍｐｌｅ．ｃｏｍ",
		"卡４１１１　１１１１　１１１１　１１１１",
		"IP：１９２．１６８．１．１０",
		"OTP：１２３４５６",
	}, "；")
	want := "身份证<身份证号>；电话<手机号>；邮箱<邮箱>；卡<银行卡号>；IP：<IP地址>；OTP：<验证码>"

	got, err := New().Transform(input)
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	if got.Masked != want {
		t.Fatal("full-width masked output mismatch")
	}
	if len(got.Matches) != 6 {
		t.Fatalf("Matches = %#v, want six categories", got.Matches)
	}
}

func TestTransformMixedRepeatedAndOverlapping(t *testing.T) {
	input := "证件 11010519491231002X，电话 13800138000 或 13900139000，邮箱 a@example.com。"
	want := "证件 <身份证号>，电话 <手机号> 或 <手机号>，邮箱 <邮箱>。"
	wantMatches := []MatchSummary{
		{Category: "cn_resident_id", Count: 1},
		{Category: "cn_mobile", Count: 2},
		{Category: "email", Count: 1},
	}

	got, err := New().Transform(input)
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	if got.Masked != want || !reflect.DeepEqual(got.Matches, wantMatches) {
		t.Fatalf("mixed masked output mismatch; matches = %#v", got.Matches)
	}
}

func TestSelectCandidatesUsesPriorityThenLongestMatch(t *testing.T) {
	lowPriorityLong := candidate{
		span: span{start: 0, end: 20},
		rule: rule{id: "low", priority: 100, order: 0},
	}
	highPriorityShort := candidate{
		span: span{start: 5, end: 10},
		rule: rule{id: "high", priority: 200, order: 1},
	}
	selected := selectCandidates([]candidate{lowPriorityLong, highPriorityShort})
	if !reflect.DeepEqual(selected, []candidate{highPriorityShort}) {
		t.Fatalf("priority selection = %#v", selected)
	}

	short := candidate{
		span: span{start: 1, end: 8},
		rule: rule{id: "short", priority: 300, order: 0},
	}
	long := candidate{
		span: span{start: 0, end: 10},
		rule: rule{id: "long", priority: 300, order: 1},
	}
	selected = selectCandidates([]candidate{short, long})
	if !reflect.DeepEqual(selected, []candidate{long}) {
		t.Fatalf("longest-match selection = %#v", selected)
	}
}

func TestTransformIsIdempotent(t *testing.T) {
	engine := New()
	first, err := engine.Transform("电话 13800138000，已有 <邮箱>，验证码 123456")
	if err != nil {
		t.Fatalf("first Transform() error = %v", err)
	}
	second, err := engine.Transform(first.Masked)
	if err != nil {
		t.Fatalf("second Transform() error = %v", err)
	}
	if second.Masked != first.Masked || len(second.Matches) != 0 {
		t.Fatalf("second transform changed protected text; matches = %#v", second.Matches)
	}
}

func TestTransformResultDoesNotExposeSourceValues(t *testing.T) {
	const source = "13800138000"
	got, err := New().Transform("电话 " + source)
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), source) {
		t.Fatal("serialized result contains a source value")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(fields) != 2 || fields["masked"] == nil || fields["matches"] == nil {
		t.Fatalf("serialized result exposes an unexpected shape: %s", encoded)
	}
}

func TestTransformLargeText(t *testing.T) {
	input := strings.Repeat("这是一段不包含个人信息的普通文本。", 20_000) + " 联系 13800138000"
	got, err := New().Transform(input)
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	if !strings.HasSuffix(got.Masked, " 联系 <手机号>") {
		t.Fatalf("large Transform() did not mask suffix")
	}
	if !reflect.DeepEqual(got.Matches, []MatchSummary{{Category: "cn_mobile", Count: 1}}) {
		t.Fatalf("Matches = %#v", got.Matches)
	}
}

func TestNilEngineFailsClosed(t *testing.T) {
	var engine *builtinEngine
	got, err := engine.Transform("电话 13800138000")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Transform() error = %v, want ErrUnavailable", err)
	}
	if !reflect.DeepEqual(got, Result{}) {
		t.Fatalf("Transform() result = %#v, want zero value", got)
	}
}

func BenchmarkTransform(b *testing.B) {
	engine := New()
	input := strings.Repeat("普通业务文本，用于稳定的规则扫描性能样例。", 1_000) + " 电话 13800138000"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.Transform(input); err != nil {
			b.Fatal(err)
		}
	}
}
