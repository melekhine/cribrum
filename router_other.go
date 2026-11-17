//go:build !linux

package cribrum

import (
	"errors"
	"runtime"
)

func newRouter(Config) (router, error) {
	return nil, errors.New("cribrum: kernel routing is not supported on " + runtime.GOOS + " (Linux only)")
}
