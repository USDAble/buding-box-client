package pii

import "testing"

func TestMaskPhones(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain", input: "13800001234", want: "138****1234"},
		{name: "spaces", input: "138 0000 1234", want: "138 **** 1234"},
		{name: "hyphens", input: "138-0000-1234", want: "138-****-1234"},
		{name: "split middle", input: "138-00-00-1234", want: "138-**-**-1234"},
		{name: "multiple", input: "我的是13800001234，他的是13900005678", want: "我的是138****1234，他的是139****5678"},
		{name: "unicode spaces", input: "电话：138\u30000000\u00a01234。", want: "电话：138\u3000****\u00a01234。"},
		{name: "plus country code", input: "+8613800001234", want: "+86138****1234"},
		{name: "bare country code", input: "8613800001234", want: "86138****1234"},
		{name: "split country code", input: "86-138-0000-1234", want: "86-138-****-1234"},
		{name: "plus split country code", input: "+86 138-0000-1234", want: "+86 138-****-1234"},
		{name: "wrong prefix", input: "23800001234", want: "23800001234"},
		{name: "ten digits", input: "1380000123", want: "1380000123"},
		{name: "twelve digits", input: "138000012345", want: "138000012345"},
		{name: "numeric left boundary", input: "913800001234", want: "913800001234"},
		{name: "numeric country boundary", input: "18613800001234", want: "18613800001234"},
		{name: "full width digits", input: "１３８００００１２３４", want: "１３８００００１２３４"},
		{name: "empty", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskPhones(tt.input); got != tt.want {
				t.Fatalf("MaskPhones(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
