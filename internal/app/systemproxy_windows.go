//go:build windows

package app

import (
	"fmt"
	"os/exec"
)

func setSystemProxy(enabled bool, httpAddress string) error {
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	value := "0"
	if enabled {
		value = "1"
	}
	if err := exec.Command("reg", "add", key, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", value, "/f").Run(); err != nil {
		return fmt.Errorf("set ProxyEnable: %w", err)
	}
	if enabled {
		proxy := "http=" + httpAddress + ";https=" + httpAddress
		if err := exec.Command("reg", "add", key, "/v", "ProxyServer", "/t", "REG_SZ", "/d", proxy, "/f").Run(); err != nil {
			return fmt.Errorf("set ProxyServer: %w", err)
		}
		_ = exec.Command("reg", "add", key, "/v", "ProxyOverride", "/t", "REG_SZ", "/d", "<local>", "/f").Run()
	}
	_ = exec.Command("rundll32.exe", "user32.dll,UpdatePerUserSystemParameters").Run()
	return nil
}
