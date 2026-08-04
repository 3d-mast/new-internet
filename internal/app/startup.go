package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

// WaitForUI verifies that the embedded interface is actually reachable and is
// served by this product. This closes the old failure mode where the GUI build
// silently continued after the HTTP listener failed in a goroutine.
func (a *App) WaitForUI(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 750 * time.Millisecond}
	url := a.UIURL() + "/api/status"
	var lastErr error

	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				var status struct {
					Protocol string `json:"protocol"`
				}
				decodeErr := json.NewDecoder(resp.Body).Decode(&status)
				_ = resp.Body.Close()
				if decodeErr == nil && status.Protocol == ProtocolVersion {
					return nil
				}
				lastErr = errors.New("another service is using the interface port")
			} else {
				_ = resp.Body.Close()
				lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	if lastErr == nil {
		lastErr = errors.New("local interface did not answer")
	}
	return fmt.Errorf("interface %s is unavailable: %w", a.UIURL(), lastErr)
}

func (a *App) OpenUI() error { return OpenURL(a.UIURL()) }

// OpenURL uses multiple Windows mechanisms because rundll32 alone may fail
// silently on hardened or modified installations.
func OpenURL(url string) error {
	var commands [][]string
	switch runtime.GOOS {
	case "windows":
		commands = [][]string{
			{"explorer.exe", url},
			{"cmd.exe", "/c", "start", "", url},
			{"rundll32.exe", "url.dll,FileProtocolHandler", url},
		}
	case "darwin":
		commands = [][]string{{"open", url}}
	default:
		commands = [][]string{{"xdg-open", url}}
	}

	var lastErr error
	for _, command := range commands {
		if err := exec.Command(command[0], command[1:]...).Start(); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return fmt.Errorf("cannot open %s: %w", url, lastErr)
}
