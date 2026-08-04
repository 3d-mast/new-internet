package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/3d-mast/new-internet/internal/app"
)

func main() {
	var dataDir string
	var noBrowser bool
	var serverMode bool
	var publicEndpoints string
	var inviteTTL time.Duration
	flag.StringVar(&dataDir, "data-dir", defaultDataDir(), "directory for identity and configuration")
	flag.BoolVar(&noBrowser, "no-browser", false, "do not open the local web interface")
	flag.BoolVar(&serverMode, "server", false, "run as a zero-config public relay and exit node")
	flag.StringVar(&publicEndpoints, "public-endpoint", "", "optional comma-separated public host:port addresses")
	flag.DurationVar(&inviteTTL, "invite-ttl", 24*time.Hour, "server invitation lifetime")
	flag.Parse()

	logger := log.New(os.Stdout, "rhizome: ", log.LstdFlags|log.Lmicroseconds)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	instance, err := app.New(dataDir, logger)
	if err != nil {
		logger.Fatalf("initialization failed: %v", err)
	}
	if serverMode {
		noBrowser = true
		if err := instance.PrepareServerMode(splitEndpoints(publicEndpoints)); err != nil {
			logger.Fatalf("server mode configuration failed: %v", err)
		}
	}
	if err := instance.Start(ctx, !noBrowser); err != nil {
		logger.Fatalf("startup failed: %v", err)
	}

	fmt.Printf("%s %s node %s is running at %s\n", app.ProductName, app.ReleaseVersion, instance.NodeID(), instance.UIURL())
	if serverMode {
		token, err := instance.CreateServerInvite(inviteTTL)
		if err != nil {
			logger.Printf("cannot create server invitation: %v", err)
		} else {
			fmt.Println("\nJOIN TOKEN (copy to a client):\n" + token + "\n")
		}
	}
	<-ctx.Done()
	instance.Close()
}

func splitEndpoints(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func defaultDataDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ".rhizome"
	}
	newPath := filepath.Join(dir, "Rhizome")
	oldPath := filepath.Join(dir, "Lichen")
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		if _, oldErr := os.Stat(oldPath); oldErr == nil {
			// Reuse the existing identity and trust database instead of forcing
			// a destructive migration merely because the release was renamed.
			return oldPath
		}
	}
	return newPath
}
