//go:build darwin && cgo

package main

import "testing"

func TestDarwinWindowAddressRequiresLoopback(t *testing.T) {
	for _, address := range []string{"http://127.0.0.1:7330/", "http://localhost:7330/", "http://[::1]:7330/"} {
		if err := validateDarwinWindowAddress(address); err != nil {
			t.Errorf("local window address %q: %v", address, err)
		}
	}
	for _, address := range []string{"https://example.com/", "http://192.168.1.1:7330/", "file:///tmp/index.html", "http://user@127.0.0.1:7330/", "http://127.0.0.1/", "http://127.0.0.1:0/", "http://127.0.0.1:65536/"} {
		if err := validateDarwinWindowAddress(address); err == nil {
			t.Errorf("accepted non-app address %q", address)
		}
	}
}

func TestDarwinNavigationConfinesNativeBridgeToAppOrigin(t *testing.T) {
	base := "http://127.0.0.1:7330/"
	for _, path := range []string{"/", "/zh-CN/project/example", "/api/export/result.zip?download=1"} {
		if !darwinNavigationIsLocal(base, "http://127.0.0.1:7330"+path) {
			t.Errorf("refused app URL %s", path)
		}
	}
	for _, address := range []string{"https://127.0.0.1:7330/", "http://127.0.0.1:7331/", "http://localhost:7330/", "http://127.0.0.1.evil.example:7330/", "http://evil.example/", "file:///tmp/index.html", "javascript:alert(1)", "http://user:pass@127.0.0.1:7330/"} {
		if darwinNavigationIsLocal(base, address) {
			t.Errorf("native bridge allowed foreign URL %q", address)
		}
	}
}
