package clienttest

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/brand"
	"github.com/open-octo/octo-agent/internal/productclient"
)

// TestWriteDataPreservesTheSignedBytes is the platform-side half of the
// "sign the bytes on the wire" rule (中台交付包 §4.3: the server serialises the
// policy once, signs that byte sequence, and embeds the same sequence in the
// response).
//
// encoding/json escapes `&`, `<` and `>` by default, so a writer that leaves
// that on rewrites the very bytes the signature covers - and it does so only
// for policies that happen to contain those characters, which is why a fixture
// with bland names hides it. A model called `Tools & Agents` is all it takes.
//
// This matters to the client's cache as much as to the wire: the cache stores
// what arrived, so if what arrived was rewritten, the signature check fails
// somewhere far away from the cause.
func TestWriteDataPreservesTheSignedBytes(t *testing.T) {
	const raw = `{"displayName":{"en":"Tools & Agents <beta>"}}`

	rec := httptest.NewRecorder()
	writeData(rec, 200, productclient.PolicyEnvelope{Policy: json.RawMessage(raw)})

	body := rec.Body.String()
	if !strings.Contains(body, `Tools & Agents <beta>`) {
		t.Errorf("the platform's writer rewrote the signed bytes:\n%s", body)
	}
	// The decoded value is what the client actually parses, so assert on it too:
	// the body could contain the literal text yet still deliver something else.
	var decoded struct {
		Data productclient.PolicyEnvelope `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decode the response: %v", err)
	}
	if string(decoded.Data.Policy) != raw {
		t.Errorf("policy bytes changed in transit:\n got %s\nwant %s", decoded.Data.Policy, raw)
	}
}

// TestFixtureAudienceIsTheBuildsBrandID is the same nail as
// profile_key_test.go's, for the other identity field.
//
// The fixture policy is addressed to FixturePolicyAudience; the build expects
// the audience its own brand declares. If those two drift, a developer build
// rejects *every* catalog it fetches - a failure that reads like a broken
// control plane, not like a fixture that was edited separately.
func TestFixtureAudienceIsTheBuildsBrandID(t *testing.T) {
	if want := brand.Load().BrandID; FixturePolicyAudience != want {
		t.Fatalf("the fixture policy is addressed to %q, but this build's brand id is %q",
			FixturePolicyAudience, want)
	}
}
