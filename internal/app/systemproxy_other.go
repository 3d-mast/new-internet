//go:build !windows

package app

import "errors"

func setSystemProxy(enabled bool, httpAddress string) error {
	if enabled {
		return errors.New("system proxy integration is currently available on Windows only")
	}
	return nil
}
