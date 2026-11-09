//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=13.0
#cgo LDFLAGS: -framework Cocoa -framework WebKit -mmacosx-version-min=13.0
#include <stdlib.h>
#include "window_darwin.h"
*/
import "C"

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"unsafe"
)

func init() { runtime.LockOSThread() }

type darwinWindow struct{}

func newWindow(address, dataDir string) (window, error) {
	if err := validateDarwinWindowAddress(address); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("the window's data folder: %w", err)
	}
	value := C.CString(address)
	defer C.free(unsafe.Pointer(value))
	if C.hw_window_create(value) == 0 {
		return nil, fmt.Errorf("create the macOS WebKit window: %w", errNoWindow)
	}
	return &darwinWindow{}, nil
}

func validateDarwinWindowAddress(address string) error {
	u, err := url.Parse(address)
	if err != nil {
		return fmt.Errorf("invalid window address: %w", err)
	}
	ip := net.ParseIP(u.Hostname())
	port, portErr := strconv.Atoi(u.Port())
	if u.Scheme != "http" || u.User != nil || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) || portErr != nil || port < 1 || port > 65535 {
		return fmt.Errorf("the program window requires a loopback HTTP address with a port")
	}
	return nil
}

func (*darwinWindow) run()   { C.hw_window_run() }
func (*darwinWindow) close() { C.hw_window_close() }
func (*darwinWindow) show()  { C.hw_window_show() }
func allowForeground()       {}

func darwinNavigationIsLocal(base, candidate string) bool {
	b, c := C.CString(base), C.CString(candidate)
	defer C.free(unsafe.Pointer(b))
	defer C.free(unsafe.Pointer(c))
	return C.hw_window_navigation_is_local(b, c) != 0
}
