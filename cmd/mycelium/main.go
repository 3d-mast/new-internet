package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/3d-mast/new-internet/internal/app"
)

func main() {
	var dataDir string
	var noBrowser bool
	flag.StringVar(&dataDir, "data-dir", defaultDataDir(), "directory for identity and configuration")
	flag.BoolVar(&noBrowser, "no-browser", false, "do not open the local web interface")
	flag.Parse()

	logger := log.New(os.Stdout, "mycelium: ", log.LstdFlags|log.Lmicroseconds)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	instance, err := app.New(dataDir, logger)
	if err != nil {
		logger.Fatalf("initialization failed: %v", err)
	}

	if err := instance.Start(ctx, !noBrowser); err != nil {
		logger.Fatalf("startup failed: %v", err)
	}

	fmt.Printf("MYCELIUM/1 node %s is running at %s\n", instance.NodeID(), instance.UIURL())
	<-ctx.Done()
	instance.Close()
}

func defaultDataDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ".mycelium"
	}
	return filepath.Join(dir, "MyceliumOne")
}
