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
