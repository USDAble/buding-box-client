package pii

import (
	"encoding/pem"
	"net"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type rule struct {
	id          string
	placeholder string
	priority    int
	order       int
	find        func(scanText) []span
}

var builtinRules = []rule{
	{id: "cn_resident_id", placeholder: "<身份证号>", priority: 400, find: findResidentIDs},
	{id: "cn_mobile", placeholder: "<手机号>", priority: 200, find: findMobileNumbers},
	{id: "email", placeholder: "<邮箱>", priority: 200, find: findEmails},
	{id: "bank_card", placeholder: "<银行卡号>", priority: 300, find: findBankCards},
	{id: "vin", placeholder: "<VIN>", priority: 400, find: findVINs},
	{id: "ip_address", placeholder: "<IP地址>", priority: 180, find: findIPAddresses},
	{id: "pem_private_key", placeholder: "<私钥>", priority: 500, find: findPrivateKeys},
	{id: "secret_token", placeholder: "<访问凭据>", priority: 500, find: findSecretTokens},
	{id: "sms_otp", placeholder: "<验证码>", priority: 100, find: findOTPs},
}

func init() {
	for i := range builtinRules {
		builtinRules[i].order = i
	}
}

var placeholders = func() []string {
	out := make([]string, len(builtinRules))
	for i, r := range builtinRules {
		out[i] = r.placeholder
	}
	return out
}()

func placeholderSpans(text string) []span {
	var out []span
	for _, placeholder := range placeholders {
		for offset := 0; ; {
			i := strings.Index(text[offset:], placeholder)
			if i < 0 {
				break
			}
			start := offset + i
			out = append(out, span{start, start + len(placeholder)})
			offset = start + len(placeholder)
		}
	}
	return out
}

var (
	residentIDRE = regexp.MustCompile(`[0-9]{17}[0-9Xx]`)
	mobileRE     = regexp.MustCompile(`1[3-9](?:[ -]?[0-9]){9}`)
	emailRE      = regexp.MustCompile("[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,63}")
	bankCardRE   = regexp.MustCompile(`[0-9](?:[ -]?[0-9]){11,18}`)
	vinRE        = regexp.MustCompile(`[A-HJ-NPR-Z0-9]{17}`)
	ipv4RE       = regexp.MustCompile(`(?:[0-9]{1,3}\.){3}[0-9]{1,3}`)
	ipTokenRE    = regexp.MustCompile(`[0-9A-Fa-f:.]{2,64}`)
	otpRE        = regexp.MustCompile(`[0-9]{4,8}`)
)

func regexMatches(scan scanText, re *regexp.Regexp, valid func(string, int, int) bool) []span {
	indices := re.FindAllStringIndex(scan.normalized, -1)
	out := make([]span, 0, len(indices))
	for _, idx := range indices {
		if valid != nil && !valid(scan.normalized, idx[0], idx[1]) {
			continue
		}
		out = append(out, scan.originalSpan(idx[0], idx[1]))
	}
	return out
}

func findResidentIDs(scan scanText) []span {
	return regexMatches(scan, residentIDRE, func(text string, start, end int) bool {
		return asciiWordBounded(text, start, end) && validResidentID(text[start:end])
	})
}

func findMobileNumbers(scan scanText) []span {
	return regexMatches(scan, mobileRE, func(text string, start, end int) bool {
		if !numericBounded(text, start, end) {
			return false
		}
		digits := stripNumberSeparators(text[start:end])
		return len(digits) == 11 && digits[0] == '1' && digits[1] >= '3' && digits[1] <= '9'
	})
}

func findEmails(scan scanText) []span {
	indices := emailRE.FindAllStringIndex(scan.normalized, -1)
	out := make([]span, 0, len(indices))
	for _, idx := range indices {
		start, end := idx[0], idx[1]
		candidate := scan.normalized[start:end]
		at := strings.LastIndexByte(candidate, '@')
		if at < 1 {
			continue
		}
		for start < idx[0]+at && scan.normalized[start] == '.' {
			start++
		}
		// A local part ending in a dot is invalid. Reject it instead of trimming
		// the dot and silently reinterpreting a different address.
		if idx[0]+at <= start || scan.normalized[idx[0]+at-1] == '.' {
			continue
		}
		address := scan.normalized[start:end]
		if asciiEmailBounded(scan.normalized, start, end) && validEmail(address) {
			out = append(out, scan.originalSpan(start, end))
		}
	}
	return out
}

func findBankCards(scan scanText) []span {
	return regexMatches(scan, bankCardRE, func(text string, start, end int) bool {
		if !numericBounded(text, start, end) {
			return false
		}
		digits := stripNumberSeparators(text[start:end])
		// Do not reinterpret the numeric prefix of an invalid 18-character
		// resident-ID candidate ending in X as a bank card.
		if len(digits) == 17 && end < len(text) && (text[end] == 'X' || text[end] == 'x') {
			return false
		}
		return len(digits) >= 12 && len(digits) <= 19 && !allSame(digits) && validLuhn(digits)
	})
}

func findVINs(scan scanText) []span {
	return regexMatches(scan, vinRE, func(text string, start, end int) bool {
		return asciiWordBounded(text, start, end) && validVIN(text[start:end])
	})
}

func findIPAddresses(scan scanText) []span {
	var out []span
	for _, idx := range ipv4RE.FindAllStringIndex(scan.normalized, -1) {
		value := scan.normalized[idx[0]:idx[1]]
		if ipBounded(scan.normalized, idx[0], idx[1]) && !versionContext(scan.normalized, idx[0]) && net.ParseIP(value) != nil {
			out = append(out, scan.originalSpan(idx[0], idx[1]))
		}
	}

	for _, token := range ipTokenRE.FindAllStringIndex(scan.normalized, -1) {
		value := scan.normalized[token[0]:token[1]]
		if !strings.Contains(value, ":") {
			continue
		}
		bestStart, bestEnd := -1, -1
		for start := 0; start < len(value); start++ {
			for end := len(value); end > start; end-- {
				if bestStart >= 0 && end-start <= bestEnd-bestStart {
					break
				}
				part := value[start:end]
				if strings.Count(part, ":") < 2 {
					continue
				}
				ip := net.ParseIP(part)
				globalStart, globalEnd := token[0]+start, token[0]+end
				if ip == nil || ip.To4() != nil || !ipv6Bounded(scan.normalized, globalStart, globalEnd) {
					continue
				}
				bestStart, bestEnd = start, end
			}
		}
		if bestStart >= 0 {
			out = append(out, scan.originalSpan(token[0]+bestStart, token[0]+bestEnd))
		}
	}
	return out
}

var privateKeyTypes = map[string]bool{
	"PRIVATE KEY":           true,
	"ENCRYPTED PRIVATE KEY": true,
	"RSA PRIVATE KEY":       true,
	"EC PRIVATE KEY":        true,
	"DSA PRIVATE KEY":       true,
	"OPENSSH PRIVATE KEY":   true,
}

func findPrivateKeys(scan scanText) []span {
	text := scan.original
	var out []span
	for offset := 0; offset < len(text); {
		rel := strings.Index(text[offset:], "-----BEGIN ")
		if rel < 0 {
			break
		}
		start := offset + rel
		block, rest := pem.Decode([]byte(text[start:]))
		if block == nil {
			offset = start + len("-----BEGIN ")
			continue
		}
		consumed := len(text[start:]) - len(rest)
		if privateKeyTypes[block.Type] && len(block.Bytes) > 0 {
			out = append(out, span{start, start + consumed})
		}
		offset = start + consumed
	}
	return out
}

var explicitSecretREs = []*regexp.Regexp{
	regexp.MustCompile(`sk-(?:ant-)?[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`AIza[A-Za-z0-9_-]{35}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{16,}`),
	regexp.MustCompile(`(?:sk|rk)_live_[A-Za-z0-9]{16,}`),
}

var contextualSecretREs = []*regexp.Regexp{
	regexp.MustCompile(`(?i)authorization\s*[:=]\s*bearer\s+([A-Za-z0-9_./+=:-]{16,})`),
	regexp.MustCompile(`(?i)bearer\s+([A-Za-z0-9_./+=:-]{16,})`),
	regexp.MustCompile(`(?i)(?:api[_ -]?key|access[_ -]?token|secret|token|密钥|令牌)\s*(?:[:=]\s*|\s+)["']?([A-Za-z0-9_./+=:-]{16,})`),
}

func findSecretTokens(scan scanText) []span {
	var out []span
	for _, re := range explicitSecretREs {
		for _, idx := range re.FindAllStringIndex(scan.normalized, -1) {
			if explicitSecretBounded(scan.normalized, idx[0], idx[1]) {
				out = append(out, scan.originalSpan(idx[0], idx[1]))
			}
		}
	}
	for _, re := range contextualSecretREs {
		for _, idx := range re.FindAllStringSubmatchIndex(scan.normalized, -1) {
			if len(idx) < 4 || idx[2] < 0 || !highEntropyValue(scan.normalized[idx[2]:idx[3]]) {
				continue
			}
			out = append(out, scan.originalSpan(idx[2], idx[3]))
		}
	}
	return out
}

var otpKeywords = []string{"验证码", "动态码", "otp", "verification code"}

func findOTPs(scan scanText) []span {
	var out []span
	for _, idx := range otpRE.FindAllStringIndex(scan.normalized, -1) {
		if !numericBounded(scan.normalized, idx[0], idx[1]) {
			continue
		}
		value := scan.normalized[idx[0]:idx[1]]
		local := strings.ToLower(runeWindow(scan.normalized, idx[0], idx[1], 8))
		if (len(value) == 4 && isYear(value) && strings.Contains(local, "年")) ||
			strings.Contains(local, "端口") || strings.Contains(local, "port") ||
			strings.Contains(local, "金额") || strings.Contains(local, "元") || strings.Contains(local, "块") {
			continue
		}
		context := strings.ToLower(runeWindow(scan.normalized, idx[0], idx[1], 32))
		matched := false
		for _, keyword := range otpKeywords {
			if strings.Contains(context, keyword) {
				matched = true
				break
			}
		}
		if matched {
			out = append(out, scan.originalSpan(idx[0], idx[1]))
		}
	}
	return out
}

func asciiWordBounded(text string, start, end int) bool {
	return (start == 0 || !asciiWord(text[start-1])) && (end == len(text) || !asciiWord(text[end]))
}

func asciiEmailBounded(text string, start, end int) bool {
	return (start == 0 || !emailChar(text[start-1])) && (end == len(text) || !emailChar(text[end]))
}

func numericBounded(text string, start, end int) bool {
	return (start == 0 || !asciiDigit(text[start-1])) && (end == len(text) || !asciiDigit(text[end]))
}

func ipBounded(text string, start, end int) bool {
	// A preceding colon is a valid label separator ("IP:192.0.2.1") and can
	// also introduce an IPv4 tail inside an IPv6 address. Overlap resolution
	// keeps the longer valid IPv6 match in the latter case.
	beforeOK := start == 0 || (!asciiWord(text[start-1]) && text[start-1] != '.')
	afterOK := end == len(text) || (!asciiWord(text[end]) && text[end] != '.')
	return beforeOK && afterOK
}

func ipv6Bounded(text string, start, end int) bool {
	// A colon before the match may be punctuation in text such as
	// "IP:2001:db8::1". A colon after the match, however, means the parser only
	// accepted a prefix of a longer colon-delimited token.
	beforeOK := start == 0 || (!asciiWord(text[start-1]) && text[start-1] != '.')
	afterOK := end == len(text) || (!asciiWord(text[end]) && text[end] != '.' && text[end] != ':')
	return beforeOK && afterOK
}

func explicitSecretBounded(text string, start, end int) bool {
	beforeOK := start == 0 || (!asciiWord(text[start-1]) && text[start-1] != '-')
	afterOK := end == len(text) || (!asciiWord(text[end]) && text[end] != '_' && text[end] != '-')
	return beforeOK && afterOK
}

func asciiDigit(b byte) bool { return b >= '0' && b <= '9' }

func asciiWord(b byte) bool {
	return asciiDigit(b) || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b == '_'
}

func emailChar(b byte) bool {
	return asciiWord(b) || strings.ContainsRune(".!#$%&'*+/=?^`{|}~@-", rune(b))
}

func stripNumberSeparators(value string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, value)
}

func allSame(value string) bool {
	for i := 1; i < len(value); i++ {
		if value[i] != value[0] {
			return false
		}
	}
	return len(value) > 0
}

func versionContext(text string, start int) bool {
	prefix := strings.ToLower(runePrefix(text, start, 10))
	prefix = strings.TrimSpace(prefix)
	return strings.HasSuffix(prefix, "version") || strings.HasSuffix(prefix, "版本") || strings.HasSuffix(prefix, "v")
}

func runePrefix(text string, end, radius int) string {
	left := end
	for count := 0; left > 0 && count < radius; count++ {
		_, size := utf8.DecodeLastRuneInString(text[:left])
		if size == 0 {
			break
		}
		left -= size
		if sentenceBoundaryAt(text, left) {
			left += size
			break
		}
	}
	return text[left:end]
}

func highEntropyValue(value string) bool {
	if len(value) < 16 {
		return false
	}
	unique := make(map[byte]struct{}, len(value))
	hasLetter, hasDigit := false, false
	for i := 0; i < len(value); i++ {
		unique[value[i]] = struct{}{}
		hasLetter = hasLetter || value[i] >= 'A' && value[i] <= 'Z' || value[i] >= 'a' && value[i] <= 'z'
		hasDigit = hasDigit || asciiDigit(value[i])
	}
	return hasLetter && hasDigit && len(unique) >= 8
}

func isYear(value string) bool {
	year, err := strconv.Atoi(value)
	return err == nil && year >= 1900 && year <= 2099
}

func runeWindow(text string, start, end, radius int) string {
	left := start
	for count := 0; left > 0 && count < radius; count++ {
		_, size := utf8.DecodeLastRuneInString(text[:left])
		if size == 0 {
			break
		}
		left -= size
		if sentenceBoundaryAt(text, left) {
			left += size
			break
		}
	}
	right := end
	for count := 0; right < len(text) && count < radius; count++ {
		_, size := utf8.DecodeRuneInString(text[right:])
		if size == 0 {
			break
		}
		if sentenceBoundaryAt(text, right) {
			break
		}
		right += size
	}
	return text[left:right]
}

func sentenceBoundaryAt(text string, offset int) bool {
	r, _ := utf8.DecodeRuneInString(text[offset:])
	return strings.ContainsRune("。！？!?\n\r;；", r)
}
