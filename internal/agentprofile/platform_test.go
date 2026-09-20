package agentprofile

import "testing"

func TestPlatformReferencePreservesPublicationAndRejectsMalformedIdentity(t *testing.T) {
	id, version, ok := PlatformReference("platform:expert_abc-123:7")
	if !ok || id != "expert_abc-123" || version != 7 {
		t.Fatalf("unexpected reference %q %d %v", id, version, ok)
	}
	for _, value := range []string{"platform:x:0", "platform:x:-1", "platform:../x:1", "platform:x:4294967296", "default", "platform:x:1:2"} {
		if _, _, ok := PlatformReference(value); ok {
			t.Fatalf("accepted malformed reference %q", value)
		}
	}
}
