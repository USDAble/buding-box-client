package browser

import (
	"sync/atomic"
	"testing"
	"unsafe"
)

func TestCDPClientNextIDIs64BitAligned(t *testing.T) {
	client := new(cdpClient)
	address := uintptr(unsafe.Pointer(&client.nextID))
	if address%8 != 0 {
		t.Fatalf("nextID address %x is not 64-bit aligned", address)
	}
	if got := atomic.AddInt64(&client.nextID, 1); got != 1 {
		t.Fatalf("first request ID = %d, want 1", got)
	}
}
