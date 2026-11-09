//go:build !windows && (!darwin || !cgo)

package main

func newWindow(url, dataDir string) (window, error) {
	return nil, errNoWindow
}

func allowForeground() {}
