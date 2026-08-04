package app

import (
	"context"
	"errors"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Autopilot continuously chooses the best available exit, enables the local
// proxies only when a healthy route exists and removes the Windows system
// proxy immediately when every exit disappears. This avoids the most common
// zero-touch failure mode: leaving the user's internet pointed at a dead proxy.
type Autopilot struct {
	app    *App
	mu     sync.RWMutex
	status AutopilotStatus
}

func NewAutopilot(app *App) *Autopilot {
	return &Autopilot{app: app, status: AutopilotStatus{Reason: "инициализация"}}
}

func (a *Autopilot) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		a.reconcile()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.reconcile()
			}
		}
	}()
}

func (a *Autopilot) Status() AutopilotStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

func (a *Autopilot) setStatus(status AutopilotStatus) {
	a.mu.Lock()
	a.status = status
	a.mu.Unlock()
}

func (a *Autopilot) reconcile() {
	cfg := a.app.store.Snapshot()
	if !cfg.Autopilot {
		a.setStatus(AutopilotStatus{Enabled: false, Active: cfg.ProxyEnabled, SelectedExit: cfg.SelectedExit, Reason: "автопилот выключен"})
		return
	}

	exitID, health := selectBestExit(cfg, a.app.node.Health())
	if exitID == "" {
		a.disableDeadRoute(cfg)
		a.setStatus(AutopilotStatus{Enabled: true, Reason: "ожидание доверенного выходного узла"})
		return
	}

	if !a.app.socks.Running() || !a.app.httpProxy.Running() {
		if err := a.app.startProxies(); err != nil {
			a.setStatus(AutopilotStatus{Enabled: true, SelectedExit: exitID, Reason: "локальный прокси не запущен: " + err.Error()})
			return
		}
	}

	systemProxy := runtime.GOOS == "windows"
	if systemProxy {
		if err := setSystemProxy(true, cfg.HTTPListen); err != nil {
			a.setStatus(AutopilotStatus{Enabled: true, SelectedExit: exitID, Route: health.Route, LatencyMS: health.LatencyMS, Reason: "не удалось включить системный прокси: " + err.Error()})
			return
		}
	}

	changed := cfg.SelectedExit != exitID || !cfg.ProxyEnabled || cfg.SystemProxy != systemProxy
	if changed {
		_ = a.app.store.Update(func(next *Config) error {
			next.SelectedExit = exitID
			next.ProxyEnabled = true
			next.SystemProxy = systemProxy
			return nil
		})
		message := "Автопилот выбрал выходной узел " + shortID(exitID)
		if strings.HasPrefix(health.Route, "relay:") {
			message += " через защищённый relay"
		}
		a.app.events.Add("success", "autopilot", message, exitID)
	}

	a.setStatus(AutopilotStatus{
		Enabled: true, Active: true, SelectedExit: exitID,
		Route: health.Route, LatencyMS: health.LatencyMS, Reason: "маршрут выбран автоматически",
	})
}

func (a *Autopilot) disableDeadRoute(cfg Config) {
	if !cfg.ProxyEnabled && !cfg.SystemProxy && cfg.SelectedExit == "" {
		return
	}
	if cfg.SystemProxy {
		_ = setSystemProxy(false, cfg.HTTPListen)
	}
	a.app.socks.Stop()
	a.app.httpProxy.Stop()
	_ = a.app.store.Update(func(next *Config) error {
		next.ProxyEnabled = false
		next.SystemProxy = false
		next.SelectedExit = ""
		return nil
	})
	a.app.events.Add("warning", "autopilot", "Доступные выходные узлы исчезли — системный прокси безопасно отключён", "")
}

func selectBestExit(cfg Config, health map[string]PeerHealth) (string, PeerHealth) {
	type candidate struct {
		id     string
		health PeerHealth
		score  int64
	}
	items := make([]candidate, 0)
	for id, peer := range cfg.Peers {
		if !peer.Capabilities.UseExit {
			continue
		}
		h := health[id]
		if !h.Online {
			continue
		}
		score := h.LatencyMS
		if score <= 0 {
			score = 1
		}
		if strings.HasPrefix(h.Route, "relay:") {
			score += 150
		}
		if strings.Contains(h.Route, "backhaul") {
			score += 80
		}
		// Hysteresis keeps a healthy current route unless another one is
		// meaningfully better, preventing constant proxy flapping.
		if id == cfg.SelectedExit {
			score -= 25
		}
		items = append(items, candidate{id: id, health: h, score: score})
	}
	if len(items) == 0 {
		return "", PeerHealth{}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].id < items[j].id
		}
		return items[i].score < items[j].score
	})
	return items[0].id, items[0].health
}

func (a *App) SetAutopilot(enabled bool) error {
	if a.ctx == nil {
		return errors.New("application is not running")
	}
	if enabled {
		if err := a.store.Update(func(cfg *Config) error {
			cfg.Autopilot = true
			cfg.AutoRelay = true
			return nil
		}); err != nil {
			return err
		}
		a.autopilot.reconcile()
		a.events.Add("success", "autopilot", "Автопилот включён", "")
		return nil
	}

	cfg := a.store.Snapshot()
	if cfg.SystemProxy {
		_ = setSystemProxy(false, cfg.HTTPListen)
	}
	a.socks.Stop()
	a.httpProxy.Stop()
	if err := a.store.Update(func(next *Config) error {
		next.Autopilot = false
		next.ProxyEnabled = false
		next.SystemProxy = false
		next.SelectedExit = ""
		return nil
	}); err != nil {
		return err
	}
	a.autopilot.setStatus(AutopilotStatus{Enabled: false, Reason: "сеть остановлена пользователем"})
	a.events.Add("info", "autopilot", "Сеть и системный прокси остановлены", "")
	return nil
}
