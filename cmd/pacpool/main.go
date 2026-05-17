package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Pingancoin/pacpool/internal/api"
	"github.com/Pingancoin/pacpool/internal/service"
	"github.com/Pingancoin/pacpool/internal/upstream"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:9809", "HTTP listen address")
	pacdURL := flag.String("pacd", "http://127.0.0.1:9509", "pacd RPC URL")
	pacdataURL := flag.String("pacdata", "http://127.0.0.1:9609", "pacdata URL")
	interval := flag.Duration("interval", 5*time.Second, "upstream refresh interval")
	feeBPS := flag.Int("feebps", 500, "pool fee in basis points")
	flag.Parse()

	svc := service.New(upstream.NewPACD(*pacdURL), upstream.NewPACData(*pacdataURL), *interval, *feeBPS)
	server := &http.Server{
		Addr:              *listen,
		Handler:           api.New(svc).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := svc.Run(ctx); err != nil {
			exit(err)
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("pacpool listening on http://%s", *listen)
		log.Printf("pacpool upstream pacd=%s pacdata=%s", *pacdURL, *pacdataURL)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		exit(err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		exit(err)
	}
}

func exit(err error) {
	fmt.Fprintln(os.Stderr, "pacpool:", err)
	os.Exit(1)
}
