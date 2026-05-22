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
	"github.com/Pingancoin/pacpool/internal/stratum"
	"github.com/Pingancoin/pacpool/internal/upstream"
	"github.com/Pingancoin/pacpool/internal/walletclient"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:9809", "HTTP listen address")
	pacdURL := flag.String("pacd", "http://127.0.0.1:9509", "pacd RPC URL")
	pacdataURL := flag.String("pacdata", "http://127.0.0.1:9609", "pacdata URL")
	miningAddr := flag.String("miningaddr", "", "pool payout/mining address used for block template requests")
	stratumListen := flag.String("stratumlisten", "127.0.0.1:3333", "Stratum TCP listen address")
	shareDiff := flag.Float64("sharedifficulty", 1, "base Stratum share difficulty")
	varDiff := flag.Bool("vardiff", true, "enable per-worker variable difficulty")
	varDiffTarget := flag.Duration("vardifftarget", 15*time.Second, "target time between accepted shares per worker")
	dataDir := flag.String("datadir", "./data", "pool data directory for persistent share ledger")
	interval := flag.Duration("interval", 5*time.Second, "upstream refresh interval")
	feeBPS := flag.Int("feebps", 500, "pool fee in basis points")
	adminToken := flag.String("admintoken", os.Getenv("PACPOOL_ADMIN_TOKEN"), "admin token required for payout execution; defaults to PACPOOL_ADMIN_TOKEN")
	autoPayout := flag.Bool("autopayout", envBool("PACPOOL_AUTO_PAYOUT", false), "enable automatic wallet payout execution")
	payoutWalletURL := flag.String("payoutwallet", os.Getenv("PACPOOL_WALLET_URL"), "local pacwallet service URL used for automatic payouts")
	payoutWalletToken := flag.String("payoutwallettoken", os.Getenv("PACPOOL_WALLET_TOKEN"), "optional token sent to the local wallet service")
	payoutPassphrase := flag.String("payoutpassphrase", os.Getenv("PACPOOL_WALLET_PASSPHRASE"), "optional wallet passphrase for automatic payouts")
	payoutFee := flag.String("payoutfee", envString("PACPOOL_PAYOUT_FEE", "0.0001"), "wallet transaction fee for automatic payout batches")
	payoutMin := flag.Int64("payoutminatoms", envInt64("PACPOOL_PAYOUT_MIN_ATOMS", 0), "minimum total pending payout atoms before automatic payout")
	payoutEvery := flag.Duration("payoutevery", envDuration("PACPOOL_PAYOUT_INTERVAL", time.Hour), "automatic payout interval")
	payoutWindowStart := flag.String("payoutwindowstart", envString("PACPOOL_PAYOUT_WINDOW_START", "08:00"), "automatic payout window start in HH:MM")
	payoutWindowEnd := flag.String("payoutwindowend", envString("PACPOOL_PAYOUT_WINDOW_END", "09:00"), "automatic payout window end in HH:MM")
	payoutTimezone := flag.String("payouttimezone", envString("PACPOOL_PAYOUT_TIMEZONE", "Asia/Shanghai"), "automatic payout window timezone")
	payoutBatchLimit := flag.Int("payoutbatchlimit", envInt("PACPOOL_PAYOUT_BATCH_LIMIT", 50), "maximum miner payouts per automatic payout batch")
	flag.Parse()

	var payoutSender service.PayoutSender
	if *autoPayout || *payoutWalletURL != "" {
		wallet, err := walletclient.New(walletclient.Options{
			BaseURL:    *payoutWalletURL,
			Token:      *payoutWalletToken,
			Passphrase: *payoutPassphrase,
			Fee:        *payoutFee,
		})
		if err != nil {
			exit(err)
		}
		payoutSender = wallet
	}

	svc, err := service.New(upstream.NewPACD(*pacdURL), upstream.NewPACData(*pacdataURL), service.Options{
		Interval:          *interval,
		FeeBPS:            *feeBPS,
		MiningAddr:        *miningAddr,
		ShareDiff:         *shareDiff,
		VarDiff:           *varDiff,
		VarDiffTarget:     *varDiffTarget,
		DataDir:           *dataDir,
		AutoPayout:        *autoPayout,
		PayoutMin:         *payoutMin,
		PayoutEvery:       *payoutEvery,
		PayoutWindowStart: *payoutWindowStart,
		PayoutWindowEnd:   *payoutWindowEnd,
		PayoutTimezone:    *payoutTimezone,
		PayoutBatchLimit:  *payoutBatchLimit,
		PayoutSender:      payoutSender,
	})
	if err != nil {
		exit(err)
	}
	server := &http.Server{
		Addr:              *listen,
		Handler:           api.New(svc, api.Options{AdminToken: *adminToken}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := svc.Run(ctx); err != nil {
			exit(err)
		}
	}()

	if *miningAddr != "" {
		stratumServer := stratum.New(*stratumListen, svc)
		go func() {
			if err := stratumServer.Run(ctx); err != nil {
				exit(err)
			}
		}()
		log.Printf("pacpool stratum listening on stratum+tcp://%s", *stratumListen)
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("pacpool listening on http://%s", *listen)
		log.Printf("pacpool upstream pacd=%s pacdata=%s miningaddr=%s sharedifficulty=%.4f vardiff=%t vardifftarget=%s datadir=%s", *pacdURL, *pacdataURL, *miningAddr, *shareDiff, *varDiff, *varDiffTarget, *dataDir)
		if *autoPayout {
			log.Printf("pacpool automatic payouts enabled wallet=%s interval=%s minatoms=%d fee=%s window=%s-%s timezone=%s batchlimit=%d", *payoutWalletURL, *payoutEvery, *payoutMin, *payoutFee, *payoutWindowStart, *payoutWindowEnd, *payoutTimezone, *payoutBatchLimit)
		}
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

func envString(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	case "0", "false", "FALSE", "no", "NO", "off", "OFF":
		return false
	default:
		return fallback
	}
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt64(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	var parsed int64
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}
