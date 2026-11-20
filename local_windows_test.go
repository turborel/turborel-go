//go:build windows

package turborel

import (
	"context"
	"syscall"
	"testing"
	"time"
	"unsafe" //nolint:depguard // R-GO-1 holds of the driver; this Windows test makes a pipe by the Windows API, which takes a pointer.
)

// The arguments of CreateNamedPipeW that make one instance of a duplex
// byte pipe.
const (
	pipeAccessDuplex   = 0x00000003
	pipeTypeByte       = 0x00000000
	pipeOneInstance    = 1
	pipeBufferBytes    = 4096
	pipeDefaultTimeout = 0
)

// makePipe makes one instance of the named pipe path, closed as the test
// ends.
func makePipe(t *testing.T, path string) {
	t.Helper()

	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}

	create := syscall.NewLazyDLL("kernel32.dll").NewProc("CreateNamedPipeW")

	h, _, callErr := create.Call(uintptr(unsafe.Pointer(name)), pipeAccessDuplex, pipeTypeByte, pipeOneInstance,
		pipeBufferBytes, pipeBufferBytes, pipeDefaultTimeout, 0)
	if syscall.Handle(h) == syscall.InvalidHandle {
		t.Fatalf("CreateNamedPipeW: %v", callErr)
	}

	t.Cleanup(func() { _ = syscall.CloseHandle(syscall.Handle(h)) })
}

// TestNamedPipe opens a named pipe, and gives up on one busy past the
// connect timeout.
//
// Verifies: [CLIENT Timeouts] on Windows a local connection is a named
// pipe, opened while one of its instances is free; one busy is tried
// again until the connect timeout runs out, then fails saying the server
// did not answer in time; a pipe that does not exist fails at once
// [CLIENT Connecting].
func TestNamedPipe(t *testing.T) {
	t.Parallel()

	path := `\\.\pipe\turborel-go-test-` + t.Name()
	makePipe(t, path)

	first, err := openLocal(context.Background(), path, time.Second)
	if err != nil {
		t.Fatalf("the free instance: %v", err)
	}

	t.Cleanup(func() { _ = first.Close() })

	_, err = openLocal(context.Background(), path, 50*time.Millisecond)
	assertBroken(t, err, "did not answer in time")

	_, err = openLocal(context.Background(), `\\.\pipe\turborel-go-none`, time.Second)
	assertBroken(t, err, "cannot connect")
}
