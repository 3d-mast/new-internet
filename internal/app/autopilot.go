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
// proxy immediately when every exit disappears. Canopy adds two guardrails:
// route-change confirmation to avoid flapping on one noisy probe and bounded
// retry backoff when a local proxy/system integration step fails.
type Autopilot struct {
	app    *App
	mu     sync.RWMutex
	status AutopilotStatus

	pendingExit  string
	pendingCount int
	failures     int
	nextRetry    time.Time
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
		a.resetTransitionState()
		a.setStatus(AutopilotStatus{Enabled: false, Active: cfg.ProxyEnabled, SelectedExit: cfg.SelectedExit, Reason: "автопилот выключен"})
		return
	}

	healthMap := a.app.node.Health()
	exitID, health := selectBestExit(cfg, healthMap)
	if exitID == "" {
		a.resetTransitionState()
		a.disableDeadRoute(cfg)
		a.setStatus(AutopilotStatus{Enabled: true, Reason: "ожидание доверенного выходного узла"})
		return
	}

	if !a.routeCandidateReady(cfg, healthMap, exitID) {
		current := healthMap[cfg.SelectedExit]
		a.setStatus(AutopilotStatus{
			Enabled: true, Active: cfg.ProxyEnabled, SelectedExit: cfg.SelectedExit,
			Route: current.Route, LatencyMS: current.LatencyMS,
			Reason: "проверка более выгодного маршрута перед переключением",
		})
		return
	}

	if !a.nextRetry.IsZero() && time.Now().Before(a.nextRetry) {
		a.setStatus(AutopilotStatus{
			Enabled: true, Active: cfg.ProxyEnabled, SelectedExit: cfg.SelectedExit,
			Route: health.Route, LatencyMS: health.LatencyMS,
			Reason: "повтор локальной настройки после краткой паузы",
		})
		return
	}

	if err := a.applyRoute(cfg, exitID, health); err != nil {
		delay := retryDelay(a.failures)
		a.failures++
		a.nextRetry = time.Now().Add(delay)
		a.setStatus(AutopilotStatus{
			Enabled: true, Active: cfg.ProxyEnabled, SelectedExit: cfg.SelectedExit,
			Route: health.Route, LatencyMS: health.LatencyMS,
			Reason: "маршрут найден, локальная настройка не применена: " + err.Error(),
		})
		return
	}

	a.failures = 0
	a.nextRetry = time.Time{}
	a.setStatus(AutopilotStatus{
		Enabled: true, Active: true, SelectedExit: exitID,
		Route: health.Route, LatencyMS: health.LatencyMS, Reason: "маршрут выбран автоматически",
	})
}

func (a *Autopilot) applyRoute(previous Config, exitID string, health PeerHealth) error {
	systemProxy := runtime.GOOS == "windows"
	wasSOCKS := a.app.socks.Running()
	wasHTTP := a.app.httpProxy.Running()

	// Publish the selected exit before accepting new local proxy connections.
	// Reef started listeners first, leaving a short window where a fresh request
	// could observe an empty/old SelectedExit and fail despite a healthy route.
	if err := a.app.store.Update(func(next *Config) error {
		next.SelectedExit = exitID
		next.ProxyEnabled = true
		return nil
	}); err != nil {
		return err
	}

	if !wasSOCKS || !wasHTTP {
		if err := a.app.startProxies(); err != nil {
			_ = a.app.store.Replace(previous)
			if !wasSOCKS || !wasHTTP {
				a.app.socks.Stop()
				a.app.httpProxy.Stop()
			}
			return errors.New("локальный прокси не запущен: " + err.Error())
		}
	}

	if systemProxy {
		if err := setSystemProxy(true, previous.HTTPListen); err != nil {
			_ = a.app.store.Replace(previous)
			if !wasSOCKS || !wasHTTP {
				a.app.socks.Stop()
				a.app.httpProxy.Stop()
			}
			return errors.New("не удалось включить системный прокси: " + err.Error())
		}
	}

	changed := previous.SelectedExit != exitID || !previous.ProxyEnabled || previous.SystemProxy != systemProxy
	if err := a.app.store.Update(func(next *Config) error {
		next.SelectedExit = exitID
		next.ProxyEnabled = true
		next.SystemProxy = systemProxy
		return nil
	}); err != nil {
		return err
	}
	if changed {
		message := "Автопилот выбрал выходной узел " + shortID(exitID)
		if strings.HasPrefix(health.Route, "relay:") {
			message += " через защищённый relay"
		}
		a.app.events.Add("success", "autopilot", message, exitID)
	}
	return nil
}

func (a *Autopilot) routeCandidateReady(cfg Config, health map[string]PeerHealth, candidate string) bool {
	if cfg.SelectedExit == "" || candidate == cfg.SelectedExit || !health[cfg.SelectedExit].Online {
		a.pendingExit = ""
		a.pendingCount = 0
		return true
	}
	if a.pendingExit != candidate {
		a.pendingExit = candidate
		a.pendingCount = 1
		return false
	}
	a.pendingCount++
	if a.pendingCount < 2 {
		return false
	}
	a.pendingExit = ""
	a.pendingCount = 0
	return true
}

func (a *Autopilot) resetTransitionState() {
	a.pendingExit = ""
	a.pendingCount = 0
	a.failures = 0
	a.nextRetry = time.Time{}
}

func retryDelay(failures int) time.Duration {
	if failures < 0 {
		failures = 0
	}
	if failures > 4 {
		failures = 4
	}
	return time.Second * time.Duration(1<<failures)
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
		a.autopilot.resetTransitionState()
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
	a.autopilot.resetTransitionState()
	a.autopilot.setStatus(AutopilotStatus{Enabled: false, Reason: "сеть остановлена пользователем"})
	a.events.Add("info", "autopilot", "Сеть и системный прокси остановлены", "")
	return nil
}
