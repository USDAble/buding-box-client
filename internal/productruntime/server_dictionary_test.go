// OCTO-FORK: L-D5 delta application nails — see the product baseline D5.
package productruntime

import (
	"reflect"
	"testing"

	"github.com/open-octo/octo-agent/internal/productclient"
	"github.com/open-octo/octo-agent/internal/sensitive"
)

func TestApplyDictionaryDeltaRequiresTheAcceptedBase(t *testing.T) {
	update := productclient.SensitiveDictionary{
		Mode: "delta", BaseVersion: "43", Add: []string{"new"}, Remove: []string{"old"},
	}
	current := sensitive.ServerEntry{Version: "43", Words: []string{"old", "keep"}}
	got, err := applyDictionaryUpdate(update, current, true)
	if err != nil {
		t.Fatalf("applyDictionaryUpdate: %v", err)
	}
	if want := []string{"keep", "new"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("words = %#v, want %#v", got, want)
	}
	update.BaseVersion = "42"
	if _, err := applyDictionaryUpdate(update, current, true); err == nil {
		t.Fatal("mismatched base was accepted")
	}
}

func TestApplyEmptyFullDictionaryPreservesExplicitEmptyList(t *testing.T) {
	words, err := applyDictionaryUpdate(productclient.SensitiveDictionary{Mode: "full", Words: []string{}}, sensitive.ServerEntry{Version: "1", Words: []string{"old"}}, true)
	if err != nil || words == nil || len(words) != 0 {
		t.Fatalf("explicit empty update = %#v, %v", words, err)
	}
	if _, err := applyDictionaryUpdate(productclient.SensitiveDictionary{Mode: "full"}, sensitive.ServerEntry{}, false); err == nil {
		t.Fatal("missing words must not masquerade as an empty supplemental dictionary")
	}
}
