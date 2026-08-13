package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/3d-mast/new-internet/internal/app"
)

var activeLogPath string

func main() {
	if err := run(); err != nil {
		message := "Boreal не удалось запустить.\n\n" + err.Error()
		if activeLogPath != "" { message += "\n\nЖурнал: " + activeLogPath }
		message += "\n\nДля диагностики запустите boreal-console.exe."
		showFatalError(message)
		os.Exit(1)
	}
}

func run() error {
	var dataDir string
	var noBrowser, serverMode bool
	var publicEndpoints string
	var inviteTTL time.Duration
	flag.StringVar(&dataDir, "data-dir", defaultDataDir(), "directory for Boreal identity and configuration")
	flag.BoolVar(&noBrowser, "no-browser", false, "do not open the local web interface")
	flag.BoolVar(&serverMode, "server", false, "run as a public relay and exit node")
	flag.StringVar(&publicEndpoints, "public-endpoint", "", "optional comma-separated public host:port addresses")
	flag.DurationVar(&inviteTTL, "invite-ttl", 24*time.Hour, "server invitation lifetime")
	flag.Parse()
	if err := os.MkdirAll(dataDir, 0o700); err != nil { return fmt.Errorf("create data directory: %w", err) }
	activeLogPath = filepath.Join(dataDir, "boreal.log")
	logFile, err := os.OpenFile(activeLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil { return fmt.Errorf("open log file: %w", err) }
	defer logFile.Close()
	logger := log.New(io.MultiWriter(os.Stdout, logFile), "boreal: ", log.LstdFlags|log.Lmicroseconds)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if !serverMode && existingInterfaceReady() {
		if !noBrowser { _ = app.OpenURL(defaultUIURL()) }
		return nil
	}
	instance, err := app.New(dataDir, logger)
	if err != nil { return fmt.Errorf("initialization failed: %w", err) }
	if serverMode {
		noBrowser = true
		if err := instance.PrepareServerMode(splitEndpoints(publicEndpoints)); err != nil { return fmt.Errorf("server mode configuration failed: %w", err) }
	}
	if err := instance.Start(ctx, false); err != nil { return fmt.Errorf("startup failed: %w", err) }
	defer instance.Close()
	if err := instance.WaitForUI(ctx, 8*time.Second); err != nil { return err }
	if err := writeInterfaceShortcut(dataDir, instance.UIURL()); err != nil { logger.Printf("cannot write interface shortcut: %v", err) }
	if !noBrowser { _ = instance.OpenUI() }
	if serverMode {
		token, err := instance.CreateServerInvite(inviteTTL)
		if err != nil { logger.Printf("cannot create server invitation: %v", err) } else { fmt.Println("\nJOIN TOKEN:\n" + token + "\n") }
	}
	<-ctx.Done()
	return nil
}

func existingInterfaceReady() bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(defaultUIURL() + "/api/status")
	if err != nil { return false }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { return false }
	var status struct{ Protocol string `json:"protocol"` }
	return json.NewDecoder(resp.Body).Decode(&status) == nil && status.Protocol == app.ProtocolVersion
}

func writeInterfaceShortcut(dataDir, url string) error {
	return os.WriteFile(filepath.Join(dataDir, "OPEN-BOREAL-INTERFACE.url"), []byte("[InternetShortcut]\r\nURL="+url+"\r\n"), 0o600)
}
func defaultUIURL() string { return fmt.Sprintf("http://127.0.0.1:%d", app.DefaultAPIPort) }
func splitEndpoints(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	out := make([]string, 0, len(parts)); for _, item := range parts { if item = strings.TrimSpace(item); item != "" { out = append(out, item) } }; return out
}
func defaultDataDir() string {
	dir, err := os.UserConfigDir(); if err != nil { return ".boreal" }
	return filepath.Join(dir, "Boreal")
}
