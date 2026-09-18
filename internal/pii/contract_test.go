package pii

import (
	"os"
	"reflect"
	"regexp"
	"testing"
)

func TestRuleRegistryMatchesProductDocument(t *testing.T) {
	document, err := os.ReadFile("../../dev-docs-puddingbox/个人信息保护与私密会话.md")
	if err != nil {
		t.Fatalf("read product document: %v", err)
	}

	row := regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\|")
	matches := row.FindAllSubmatch(document, -1)
	documented := make([]string, 0, len(matches))
	for _, match := range matches {
		documented = append(documented, string(match[1]))
	}

	if got := RuleIDs(); !reflect.DeepEqual(got, documented) {
		t.Fatalf("RuleIDs() = %#v, documented IDs = %#v", got, documented)
	}
	want := []string{
		"cn_resident_id",
		"cn_mobile",
		"email",
		"bank_card",
		"vin",
		"ip_address",
		"pem_private_key",
		"secret_token",
		"sms_otp",
	}
	if got := RuleIDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("RuleIDs() = %#v, stable contract = %#v", got, want)
	}
	if RuleVersion != "builtin-1" {
		t.Fatalf("RuleVersion = %q, want builtin-1", RuleVersion)
	}
}
