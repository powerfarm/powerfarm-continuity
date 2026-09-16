// Command heartime-ingress terminates the Heartime delivery relationship for
// Continuity.
//
// It accepts occurrence evidence over HTTP, deduplicates by occurrence identity
// before any effect is claimed, hands each activation to the route that already
// owns that work, and returns the outcome to Heartime bound to the digest of
// immutable evidence. It schedules nothing and decides nothing about what the
// work is.
//
// Deliveries arrive on loopback HTTP or behind operator-terminated TLS, with
// the credential the delivering Heartime is configured to present. Outcomes go
// back through the ledger's own local command, so this process runs on the
// ledger's host.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"powerfarm.dev/continuity/v2/internal/ingress"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "ingress configuration document")
	once := flag.Bool("once", false, "advance everything already recorded, then exit without listening")
	flag.Parse()
	if *configPath == "" {
		return errors.New("-config is required")
	}
	config, err := ingress.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	receiver, err := ingress.New(config)
	if err != nil {
		return err
	}
	if *once {
		return receiver.Sweep(context.Background())
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := receiver.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
