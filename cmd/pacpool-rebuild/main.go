package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Pingancoin/pacpool/internal/service"
)

const (
	baseSubsidy       = int64(1_692_065_961)
	minerRewardPct    = int64(95)
	reductionInterval = uint32(12_288)
)

type shareSnapshot struct {
	Version          string                  `json:"version"`
	UpdatedAt        time.Time               `json:"updated_at"`
	BaseDifficulty   float64                 `json:"base_difficulty"`
	VarDiffEnabled   bool                    `json:"vardiff_enabled"`
	VarDiffTargetSec int64                   `json:"vardiff_target_sec"`
	Shares           service.ShareState      `json:"shares"`
	Workers          []service.WorkerState   `json:"workers"`
	CurrentRound     service.RoundState      `json:"current_round"`
	RecentRounds     []service.RoundState    `json:"recent_rounds"`
	Payments         []service.PaymentRecord `json:"payments"`
	NextRoundID      uint64                  `json:"next_round_id"`
}

func main() {
	dataDir := flag.String("datadir", "./data", "pacpool data directory")
	feeBPS := flag.Int("feebps", 500, "pool fee in basis points")
	payoutStartHeight := flag.Uint("payoutstartheight", 0, "ignore rounds below this block height")
	maxRecentRounds := flag.Int("maxrecentrounds", 10000, "maximum rebuilt rounds to keep")
	dryRun := flag.Bool("dryrun", false, "print rebuild summary without writing share-state.json")
	flag.Parse()

	if err := rebuild(*dataDir, *feeBPS, uint32(*payoutStartHeight), *maxRecentRounds, *dryRun); err != nil {
		fmt.Fprintf(os.Stderr, "pacpool-rebuild: %v\n", err)
		os.Exit(1)
	}
}

func rebuild(dataDir string, feeBPS int, payoutStartHeight uint32, maxRecentRounds int, dryRun bool) error {
	if feeBPS < 0 || feeBPS > 5000 {
		return fmt.Errorf("fee bps out of range: %d", feeBPS)
	}
	if maxRecentRounds <= 0 {
		maxRecentRounds = 10000
	}
	statePath := filepath.Join(dataDir, "share-state.json")
	ledgerPath := filepath.Join(dataDir, "share-events.jsonl")
	stateData, err := os.ReadFile(statePath)
	if err != nil {
		return err
	}
	var snapshot shareSnapshot
	if err := json.Unmarshal(stateData, &snapshot); err != nil {
		return fmt.Errorf("decode share state: %w", err)
	}
	rebuildMinRound := firstRetainedPaymentRound(snapshot.Payments)
	if rebuildMinRound == 0 {
		rebuildMinRound = 1
	}
	rounds, currentRound, nextRoundID, err := replayLedger(ledgerPath, feeBPS, payoutStartHeight, rebuildMinRound)
	if err != nil {
		return err
	}
	applyRetainedPayments(rounds, snapshot.Payments)
	sort.Slice(rounds, func(i, j int) bool { return rounds[i].ID > rounds[j].ID })
	if len(rounds) > maxRecentRounds {
		rounds = rounds[:maxRecentRounds]
	}
	oldPending := pendingTotal(snapshot.RecentRounds)
	newPending := pendingTotal(rounds)
	fmt.Printf("rebuilt_rounds=%d rebuild_min_round=%d old_pending_atoms=%d new_pending_atoms=%d\n", len(rounds), rebuildMinRound, oldPending, newPending)
	fmt.Printf("new_pending_pac=%.8f\n", float64(newPending)/100_000_000)
	if dryRun {
		return nil
	}
	backup := fmt.Sprintf("%s.bak.%d", statePath, time.Now().Unix())
	if err := os.WriteFile(backup, stateData, 0o600); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	snapshot.UpdatedAt = time.Now().UTC()
	snapshot.CurrentRound = currentRound
	snapshot.RecentRounds = rounds
	snapshot.NextRoundID = nextRoundID
	encoded, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	tmp := statePath + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, statePath)
}

func replayLedger(path string, feeBPS int, payoutStartHeight uint32, rebuildMinRound uint64) ([]service.RoundState, service.RoundState, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, service.RoundState{}, 0, err
	}
	defer file.Close()

	nextRoundID := uint64(1)
	current := service.RoundState{ID: nextRoundID}
	workers := make(map[string]*service.RoundWorker)
	var rounds []service.RoundState

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 8*1024*1024)
	for scanner.Scan() {
		var event service.ShareEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, service.RoundState{}, 0, fmt.Errorf("decode ledger: %w", err)
		}
		if current.StartedAt.IsZero() {
			current.StartedAt = event.Timestamp
			current.UpdatedAt = event.Timestamp
		}
		if event.Accepted && event.Difficulty > 0 {
			worker := strings.TrimSpace(event.Worker)
			if worker == "" {
				worker = "anonymous"
			}
			entry := workers[worker]
			if entry == nil {
				entry = &service.RoundWorker{Name: worker}
				workers[worker] = entry
			}
			entry.AcceptedShares++
			entry.AcceptedWork += event.Difficulty
			entry.LastShareAt = event.Timestamp
			current.AcceptedShares++
			current.AcceptedWork += event.Difficulty
			current.UpdatedAt = event.Timestamp
		}
		if event.Solved && event.BlockHeight > 0 {
			current.Solved = true
			current.FoundBy = strings.TrimSpace(event.Worker)
			current.BlockHeight = event.BlockHeight
			current.BlockHash = strings.TrimSpace(event.BlockHash)
			current.EndedAt = event.Timestamp
			current.UpdatedAt = event.Timestamp
			current.Workers = sortedRoundWorkers(workers)
			finalizeRound(&current, feeBPS)
			if current.BlockHeight >= payoutStartHeight && current.ID >= rebuildMinRound {
				rounds = append(rounds, current)
			}
			nextRoundID++
			current = service.RoundState{ID: nextRoundID, StartedAt: event.Timestamp, UpdatedAt: event.Timestamp}
			workers = make(map[string]*service.RoundWorker)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, service.RoundState{}, 0, err
	}
	current.Workers = sortedRoundWorkers(workers)
	return rounds, current, nextRoundID, nil
}

func sortedRoundWorkers(workers map[string]*service.RoundWorker) []service.RoundWorker {
	out := make([]service.RoundWorker, 0, len(workers))
	for _, worker := range workers {
		out = append(out, *worker)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func finalizeRound(round *service.RoundState, feeBPS int) {
	if round == nil || round.AcceptedWork <= 0 {
		return
	}
	round.RewardTotal = minerRewardAtHeight(round.BlockHeight)
	round.PoolFee = round.RewardTotal * int64(feeBPS) / 10000
	round.Distributable = round.RewardTotal - round.PoolFee
	if round.Distributable < 0 {
		round.Distributable = 0
	}
	round.Payouts = calculatePayouts(round.Workers, round.Distributable, round.AcceptedWork)
}

func calculatePayouts(workers []service.RoundWorker, distributable int64, totalWork float64) []service.PayoutEntry {
	if distributable <= 0 || totalWork <= 0 || len(workers) == 0 {
		return nil
	}
	payouts := make([]service.PayoutEntry, 0, len(workers))
	remainder := distributable
	for _, worker := range workers {
		share := int64(math.Floor((worker.AcceptedWork / totalWork) * float64(distributable)))
		if share < 0 {
			share = 0
		}
		payouts = append(payouts, service.PayoutEntry{
			Worker: worker.Name,
			Amount: share,
			Work:   worker.AcceptedWork,
		})
		remainder -= share
	}
	for i := 0; remainder > 0 && i < len(payouts); i++ {
		payouts[i].Amount++
		remainder--
		if i == len(payouts)-1 && remainder > 0 {
			i = -1
		}
	}
	return payouts
}

func applyRetainedPayments(rounds []service.RoundState, payments []service.PaymentRecord) {
	byID := make(map[uint64]*service.RoundState, len(rounds))
	for i := range rounds {
		byID[rounds[i].ID] = &rounds[i]
	}
	for _, payment := range payments {
		selected := make(map[string]struct{})
		for _, payout := range payment.Payouts {
			if address := payoutAddressFromWorker(payout.Worker); address != "" {
				selected[address] = struct{}{}
			}
		}
		for _, roundID := range payment.Rounds {
			round := byID[roundID]
			if round == nil {
				continue
			}
			for i := range round.Payouts {
				if _, ok := selected[payoutAddressFromWorker(round.Payouts[i].Worker)]; ok {
					round.Payouts[i].Paid = true
				}
			}
			round.Paid = roundFullyPaid(*round)
		}
	}
}

func roundFullyPaid(round service.RoundState) bool {
	if !round.Solved || len(round.Payouts) == 0 {
		return false
	}
	for _, payout := range round.Payouts {
		if payout.Amount > 0 && !payout.Paid {
			return false
		}
	}
	return true
}

func pendingTotal(rounds []service.RoundState) int64 {
	var total int64
	for _, round := range rounds {
		if round.Paid {
			continue
		}
		for _, payout := range round.Payouts {
			if !payout.Paid {
				total += payout.Amount
			}
		}
	}
	return total
}

func firstRetainedPaymentRound(payments []service.PaymentRecord) uint64 {
	var first uint64
	for _, payment := range payments {
		for _, roundID := range payment.Rounds {
			if first == 0 || roundID < first {
				first = roundID
			}
		}
	}
	return first
}

func minerRewardAtHeight(height uint32) int64 {
	if height == 0 {
		return 0
	}
	subsidy := baseSubsidy
	for reductions := height / reductionInterval; reductions > 0; reductions-- {
		subsidy *= 100
		subsidy /= 101
	}
	return subsidy * minerRewardPct / 100
}

func payoutAddressFromWorker(worker string) string {
	worker = strings.TrimSpace(worker)
	if beforeDot, _, ok := strings.Cut(worker, "."); ok {
		worker = beforeDot
	}
	return strings.TrimSpace(worker)
}
