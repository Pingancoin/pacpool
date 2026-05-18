package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Pingancoin/pacpool/internal/upstream"
)

type PACDSource interface {
	MiningInfo(context.Context) (upstream.MiningInfo, error)
	NetworkInfo(context.Context) (upstream.NetworkInfo, error)
	BlockTemplate(context.Context, string) (upstream.BlockTemplate, error)
	SubmitBlock(context.Context, string) (bool, uint32, string, error)
}

type PACDataSource interface {
	Status(context.Context) (upstream.IndexStatus, error)
}

type Service struct {
	pacd       PACDSource
	pacdata    PACDataSource
	interval   time.Duration
	feeBPS     int
	miningAddr string
	shareDiff  float64
	varDiff    bool
	varTarget  time.Duration
	varMin     float64
	varMax     float64
	dataDir    string
	ledgerPath string
	statePath  string
	now        func() time.Time

	mu               sync.RWMutex
	persistMu        sync.Mutex
	state            State
	lastTemplate     upstream.BlockTemplate
	stratumConnected int
	stratumJobs      int
	workers          map[string]*WorkerState
	nextRoundID      uint64
	currentRound     RoundState
	recentRounds     []RoundState
}

type State struct {
	UpdatedAt time.Time            `json:"updated_at"`
	PACD      upstream.MiningInfo  `json:"pacd"`
	Network   upstream.NetworkInfo `json:"network"`
	PACData   upstream.IndexStatus `json:"pacdata"`
	Pool      PoolState            `json:"pool"`
	Healthy   bool                 `json:"healthy"`
	Errors    map[string]string    `json:"errors,omitempty"`
}

type PoolState struct {
	Name             string          `json:"name"`
	Version          string          `json:"version"`
	FeePercent       float64         `json:"fee_percent"`
	ShareDifficulty  float64         `json:"share_difficulty"`
	VarDiffEnabled   bool            `json:"vardiff_enabled"`
	VarDiffTargetSec int64           `json:"vardiff_target_sec"`
	ConnectedMiners  int             `json:"connected_miners"`
	ActiveJobs       int             `json:"active_jobs"`
	ReadyForStratum  bool            `json:"ready_for_stratum"`
	TemplateBackfill bool            `json:"template_backfill"`
	MiningAddress    string          `json:"mining_address,omitempty"`
	LedgerPath       string          `json:"ledger_path,omitempty"`
	LastLedgerError  string          `json:"last_ledger_error,omitempty"`
	Template         TemplateState   `json:"template"`
	Shares           ShareState      `json:"shares"`
	Workers          []WorkerState   `json:"workers,omitempty"`
	CurrentRound     RoundState      `json:"current_round"`
	RecentRounds     []RoundState    `json:"recent_rounds,omitempty"`
	PendingPayouts   []PayoutEntry   `json:"pending_payouts,omitempty"`
	Balances         []BalanceEntry  `json:"balances,omitempty"`
	Payments         []PaymentRecord `json:"payments,omitempty"`
	Notes            []string        `json:"notes"`
}

type TemplateState struct {
	Available         bool   `json:"available"`
	Height            uint32 `json:"height"`
	PreviousBlockHash string `json:"previousblockhash,omitempty"`
	Bits              string `json:"bits,omitempty"`
	MempoolSize       int    `json:"mempoolsize"`
	TotalFees         int64  `json:"totalfees"`
	TransactionCount  int    `json:"transaction_count"`
	CoinbaseTxID      string `json:"coinbasetxid,omitempty"`
}

type ShareState struct {
	Accepted       uint64    `json:"accepted"`
	Rejected       uint64    `json:"rejected"`
	SolvedBlocks   uint64    `json:"solved_blocks"`
	LastAcceptedAt time.Time `json:"last_accepted_at,omitempty"`
	LastRejectedAt time.Time `json:"last_rejected_at,omitempty"`
	LastSolvedAt   time.Time `json:"last_solved_at,omitempty"`
}

type WorkerState struct {
	Name           string    `json:"name"`
	Difficulty     float64   `json:"difficulty"`
	Accepted       uint64    `json:"accepted"`
	AcceptedWork   float64   `json:"accepted_work"`
	Rejected       uint64    `json:"rejected"`
	SolvedBlocks   uint64    `json:"solved_blocks"`
	LastShareAt    time.Time `json:"last_share_at,omitempty"`
	LastAcceptedAt time.Time `json:"last_accepted_at,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
}

type RoundState struct {
	ID             uint64        `json:"id"`
	StartedAt      time.Time     `json:"started_at,omitempty"`
	UpdatedAt      time.Time     `json:"updated_at,omitempty"`
	EndedAt        time.Time     `json:"ended_at,omitempty"`
	AcceptedShares uint64        `json:"accepted_shares"`
	AcceptedWork   float64       `json:"accepted_work"`
	Solved         bool          `json:"solved"`
	FoundBy        string        `json:"found_by,omitempty"`
	BlockHeight    uint32        `json:"block_height,omitempty"`
	BlockHash      string        `json:"block_hash,omitempty"`
	RewardTotal    int64         `json:"reward_total,omitempty"`
	RewardFees     int64         `json:"reward_fees,omitempty"`
	PoolFee        int64         `json:"pool_fee,omitempty"`
	Distributable  int64         `json:"distributable,omitempty"`
	Paid           bool          `json:"paid"`
	Payouts        []PayoutEntry `json:"payouts,omitempty"`
	Workers        []RoundWorker `json:"workers,omitempty"`
}

type RoundWorker struct {
	Name           string    `json:"name"`
	AcceptedShares uint64    `json:"accepted_shares"`
	AcceptedWork   float64   `json:"accepted_work"`
	LastShareAt    time.Time `json:"last_share_at,omitempty"`
}

type PayoutEntry struct {
	Worker string  `json:"worker"`
	Amount int64   `json:"amount"`
	Work   float64 `json:"work"`
}

type BalanceEntry struct {
	Worker string `json:"worker"`
	Unpaid int64  `json:"unpaid"`
	Paid   int64  `json:"paid"`
	Total  int64  `json:"total"`
}

type PaymentRecord struct {
	ID        string        `json:"id"`
	CreatedAt time.Time     `json:"created_at"`
	TxID      string        `json:"txid,omitempty"`
	Note      string        `json:"note,omitempty"`
	Total     int64         `json:"total"`
	Rounds    []uint64      `json:"rounds"`
	Payouts   []PayoutEntry `json:"payouts"`
}

type Options struct {
	Interval      time.Duration
	FeeBPS        int
	MiningAddr    string
	ShareDiff     float64
	VarDiff       bool
	VarDiffTarget time.Duration
	VarDiffMin    float64
	VarDiffMax    float64
	DataDir       string
	Now           func() time.Time
}

func New(pacd PACDSource, pacdata PACDataSource, opts Options) (*Service, error) {
	if opts.Interval <= 0 {
		opts.Interval = 5 * time.Second
	}
	if opts.ShareDiff <= 0 {
		opts.ShareDiff = 1
	}
	if opts.VarDiffTarget <= 0 {
		opts.VarDiffTarget = 15 * time.Second
	}
	if opts.VarDiffMin <= 0 {
		opts.VarDiffMin = opts.ShareDiff
	}
	if opts.VarDiffMax < opts.VarDiffMin {
		opts.VarDiffMax = math.Max(opts.VarDiffMin, 1024)
	}
	state := State{
		Pool: PoolState{
			Name:             "pacpool",
			Version:          "0.1.0",
			FeePercent:       float64(opts.FeeBPS) / 100,
			ShareDifficulty:  opts.ShareDiff,
			VarDiffEnabled:   opts.VarDiff,
			VarDiffTargetSec: int64(opts.VarDiffTarget / time.Second),
			ReadyForStratum:  false,
			TemplateBackfill: false,
			MiningAddress:    opts.MiningAddr,
			Notes: []string{
				"Phase 0 control plane is live.",
				"Minimal Stratum work distribution is live.",
				"Per-worker share accounting is live.",
				"VarDiff, persistent share ledger, and payout execution are live.",
				"Next step is miner dashboards and wallet-linked payout automation.",
			},
		},
		Errors: make(map[string]string),
	}
	svc := &Service{
		pacd:        pacd,
		pacdata:     pacdata,
		interval:    opts.Interval,
		feeBPS:      opts.FeeBPS,
		miningAddr:  opts.MiningAddr,
		shareDiff:   opts.ShareDiff,
		varDiff:     opts.VarDiff,
		varTarget:   opts.VarDiffTarget,
		varMin:      opts.VarDiffMin,
		varMax:      opts.VarDiffMax,
		dataDir:     opts.DataDir,
		state:       state,
		workers:     make(map[string]*WorkerState),
		now:         opts.Now,
		nextRoundID: 1,
	}
	if svc.now == nil {
		svc.now = time.Now
	}
	svc.currentRound = svc.newRoundLocked(svc.now().UTC())
	svc.state.Pool.CurrentRound = svc.currentRound
	if err := svc.initPersistence(); err != nil {
		return nil, err
	}
	return svc, nil
}

func (s *Service) Run(ctx context.Context) error {
	s.Refresh(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.Refresh(ctx)
		}
	}
}

func (s *Service) Refresh(ctx context.Context) {
	mining, miningErr := s.pacd.MiningInfo(ctx)
	network, networkErr := s.pacd.NetworkInfo(ctx)
	index, indexErr := s.pacdata.Status(ctx)
	var template upstream.BlockTemplate
	var templateErr error
	if s.miningAddr != "" {
		template, templateErr = s.pacd.BlockTemplate(ctx, s.miningAddr)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Errors = make(map[string]string)
	s.state.UpdatedAt = s.now().UTC()
	s.state.Healthy = miningErr == nil && networkErr == nil && indexErr == nil && (s.miningAddr == "" || templateErr == nil)

	if miningErr == nil {
		s.state.PACD = mining
	} else {
		s.state.Errors["pacd_mining"] = miningErr.Error()
	}
	if networkErr == nil {
		s.state.Network = network
	} else {
		s.state.Errors["pacd_network"] = networkErr.Error()
	}
	if indexErr == nil {
		s.state.PACData = index
	} else {
		s.state.Errors["pacdata"] = indexErr.Error()
	}
	if s.miningAddr != "" {
		if templateErr == nil {
			s.lastTemplate = template
			s.state.Pool.Template = TemplateState{
				Available:         true,
				Height:            template.Height,
				PreviousBlockHash: template.PreviousBlockHash,
				Bits:              template.Bits,
				MempoolSize:       template.MempoolSize,
				TotalFees:         template.TotalFees,
				TransactionCount:  len(template.TransactionIDs),
				CoinbaseTxID:      template.CoinbaseTxID,
			}
		} else {
			s.state.Errors["pacd_template"] = templateErr.Error()
			s.lastTemplate = upstream.BlockTemplate{}
			s.state.Pool.Template = TemplateState{}
		}
	}

	s.state.Pool.ConnectedMiners = s.stratumConnected
	s.state.Pool.ActiveJobs = s.stratumJobs
	s.state.Pool.TemplateBackfill = s.state.Network.BestHeight > 0 && s.state.PACData.IndexedHeight == s.state.Network.BestHeight
	s.state.Pool.ReadyForStratum = s.miningAddr != "" && s.state.Pool.Template.Available && s.state.Pool.TemplateBackfill
}

func (s *Service) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clone := s.state
	clone.Pool.Notes = append([]string(nil), s.state.Pool.Notes...)
	clone.Pool.Workers = append([]WorkerState(nil), s.state.Pool.Workers...)
	clone.Pool.CurrentRound = cloneRoundState(s.state.Pool.CurrentRound)
	clone.Pool.RecentRounds = cloneRounds(s.state.Pool.RecentRounds)
	clone.Pool.PendingPayouts = append(make([]PayoutEntry, 0, len(s.state.Pool.PendingPayouts)), s.state.Pool.PendingPayouts...)
	clone.Pool.Balances = append(make([]BalanceEntry, 0, len(s.state.Pool.Balances)), s.state.Pool.Balances...)
	clone.Pool.Payments = clonePayments(s.state.Pool.Payments)
	if len(s.state.Errors) > 0 {
		clone.Errors = make(map[string]string, len(s.state.Errors))
		for k, v := range s.state.Errors {
			clone.Errors[k] = v
		}
	}
	return clone
}

func (s *Service) CurrentTemplate() (upstream.BlockTemplate, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.state.Pool.Template.Available {
		return upstream.BlockTemplate{}, false
	}
	template := s.lastTemplate
	template.TransactionIDs = append([]string(nil), template.TransactionIDs...)
	template.Mutable = append([]string(nil), template.Mutable...)
	return template, true
}

func (s *Service) SubmitSolvedBlock(ctx context.Context, blockHex string) (bool, uint32, string, error) {
	return s.pacd.SubmitBlock(ctx, blockHex)
}

func (s *Service) ShareDifficulty() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shareDiff
}

func (s *Service) SetStratumStats(connected int, jobs int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stratumConnected = connected
	s.stratumJobs = jobs
	s.state.Pool.ConnectedMiners = connected
	s.state.Pool.ActiveJobs = jobs
}

func (s *Service) RecordShare(worker string, accepted bool, solved bool, reason string) {
	s.mu.Lock()

	now := s.now().UTC()
	if worker == "" {
		worker = "anonymous"
	}
	ws, ok := s.workers[worker]
	if !ok {
		ws = &WorkerState{
			Name:       worker,
			Difficulty: s.shareDiff,
		}
		s.workers[worker] = ws
	}
	ws.LastShareAt = now
	shareWork := ws.Difficulty
	if shareWork <= 0 {
		shareWork = s.shareDiff
	}
	if accepted {
		s.state.Pool.Shares.Accepted++
		s.state.Pool.Shares.LastAcceptedAt = now
		ws.Accepted++
		ws.AcceptedWork += shareWork
		s.applyShareToRoundLocked(ws.Name, shareWork, now)
		if s.varDiff && !ws.LastAcceptedAt.IsZero() {
			ws.Difficulty = adjustDifficulty(ws.Difficulty, now.Sub(ws.LastAcceptedAt), s.varTarget, s.varMin, s.varMax)
		}
		ws.LastAcceptedAt = now
		ws.LastError = ""
	} else {
		s.state.Pool.Shares.Rejected++
		s.state.Pool.Shares.LastRejectedAt = now
		ws.Rejected++
		ws.LastError = reason
	}
	if solved {
		s.state.Pool.Shares.SolvedBlocks++
		s.state.Pool.Shares.LastSolvedAt = now
		ws.SolvedBlocks++
	}
	if ws.Difficulty <= 0 {
		ws.Difficulty = s.shareDiff
	}
	s.state.Pool.Workers = s.sortedWorkersLocked()
	s.state.Pool.CurrentRound = cloneRoundState(s.currentRound)
	s.state.Pool.RecentRounds = cloneRounds(s.recentRounds)
	snapshot := s.shareSnapshotLocked()
	event := ShareEvent{
		Timestamp:  now,
		Worker:     ws.Name,
		Difficulty: ws.Difficulty,
		Accepted:   accepted,
		Solved:     solved,
		Reason:     reason,
	}
	s.mu.Unlock()
	s.persistShareUpdate(event, snapshot)
}

func (s *Service) sortedWorkersLocked() []WorkerState {
	workers := make([]WorkerState, 0, len(s.workers))
	for _, worker := range s.workers {
		workers = append(workers, *worker)
	}
	sort.Slice(workers, func(i, j int) bool {
		if workers[i].SolvedBlocks != workers[j].SolvedBlocks {
			return workers[i].SolvedBlocks > workers[j].SolvedBlocks
		}
		if workers[i].AcceptedWork != workers[j].AcceptedWork {
			return workers[i].AcceptedWork > workers[j].AcceptedWork
		}
		if workers[i].Accepted != workers[j].Accepted {
			return workers[i].Accepted > workers[j].Accepted
		}
		return workers[i].Name < workers[j].Name
	})
	return workers
}

func (s *Service) WorkerDifficulty(worker string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.workerDifficultyLocked(worker)
}

func (s *Service) RecordSolvedBlock(worker string, height uint32, hash string) {
	s.mu.Lock()
	now := s.now().UTC()
	if worker == "" {
		worker = "anonymous"
	}
	if s.currentRound.ID == 0 {
		s.currentRound = s.newRoundLocked(now)
	}
	s.currentRound.Solved = true
	s.currentRound.FoundBy = worker
	s.currentRound.BlockHeight = height
	s.currentRound.BlockHash = hash
	s.currentRound.EndedAt = now
	s.currentRound.UpdatedAt = now
	s.finalizeRoundPayoutLocked(&s.currentRound)
	finished := cloneRoundState(s.currentRound)
	s.recentRounds = append([]RoundState{finished}, s.recentRounds...)
	if len(s.recentRounds) > 20 {
		s.recentRounds = s.recentRounds[:20]
	}
	s.nextRoundID++
	s.currentRound = s.newRoundLocked(now)
	s.state.Pool.Workers = s.sortedWorkersLocked()
	s.state.Pool.CurrentRound = cloneRoundState(s.currentRound)
	s.state.Pool.RecentRounds = cloneRounds(s.recentRounds)
	s.state.Pool.PendingPayouts = s.pendingPayoutsLocked()
	s.state.Pool.Balances = s.balanceEntriesLocked()
	snapshot := s.shareSnapshotLocked()
	s.mu.Unlock()
	s.persistShareUpdate(ShareEvent{
		Timestamp:   now,
		Worker:      worker,
		Accepted:    true,
		Solved:      true,
		BlockHeight: height,
		BlockHash:   hash,
	}, snapshot)
}

func (s *Service) workerDifficultyLocked(worker string) float64 {
	if worker == "" {
		return s.shareDiff
	}
	if ws, ok := s.workers[worker]; ok && ws.Difficulty > 0 {
		return ws.Difficulty
	}
	return s.shareDiff
}

func (s *Service) applyShareToRoundLocked(worker string, shareWork float64, now time.Time) {
	if s.currentRound.ID == 0 {
		s.currentRound = s.newRoundLocked(now)
	}
	s.currentRound.AcceptedShares++
	s.currentRound.AcceptedWork += shareWork
	s.currentRound.UpdatedAt = now
	found := false
	for i := range s.currentRound.Workers {
		if s.currentRound.Workers[i].Name == worker {
			s.currentRound.Workers[i].AcceptedShares++
			s.currentRound.Workers[i].AcceptedWork += shareWork
			s.currentRound.Workers[i].LastShareAt = now
			found = true
			break
		}
	}
	if !found {
		s.currentRound.Workers = append(s.currentRound.Workers, RoundWorker{
			Name:           worker,
			AcceptedShares: 1,
			AcceptedWork:   shareWork,
			LastShareAt:    now,
		})
	}
	sort.Slice(s.currentRound.Workers, func(i, j int) bool {
		if s.currentRound.Workers[i].AcceptedWork != s.currentRound.Workers[j].AcceptedWork {
			return s.currentRound.Workers[i].AcceptedWork > s.currentRound.Workers[j].AcceptedWork
		}
		if s.currentRound.Workers[i].AcceptedShares != s.currentRound.Workers[j].AcceptedShares {
			return s.currentRound.Workers[i].AcceptedShares > s.currentRound.Workers[j].AcceptedShares
		}
		return s.currentRound.Workers[i].Name < s.currentRound.Workers[j].Name
	})
}

func (s *Service) newRoundLocked(start time.Time) RoundState {
	return RoundState{
		ID:        s.nextRoundID,
		StartedAt: start,
		UpdatedAt: start,
	}
}

func cloneRoundState(round RoundState) RoundState {
	clone := round
	clone.Workers = append([]RoundWorker(nil), round.Workers...)
	clone.Payouts = append([]PayoutEntry(nil), round.Payouts...)
	return clone
}

func cloneRounds(rounds []RoundState) []RoundState {
	out := make([]RoundState, 0, len(rounds))
	for _, round := range rounds {
		out = append(out, cloneRoundState(round))
	}
	return out
}

func clonePayments(payments []PaymentRecord) []PaymentRecord {
	out := make([]PaymentRecord, 0, len(payments))
	for _, payment := range payments {
		clone := payment
		clone.Rounds = append([]uint64(nil), payment.Rounds...)
		clone.Payouts = append([]PayoutEntry(nil), payment.Payouts...)
		out = append(out, clone)
	}
	return out
}

func (s *Service) finalizeRoundPayoutLocked(round *RoundState) {
	if round == nil || round.AcceptedWork <= 0 {
		return
	}
	rewardTotal := s.lastTemplate.NextSubsidy.Miner
	if rewardTotal <= 0 {
		rewardTotal = s.state.PACD.NextSubsidy.Miner
	}
	round.RewardTotal = rewardTotal
	round.RewardFees = s.lastTemplate.TotalFees
	round.PoolFee = rewardTotal * int64(s.feeBPS) / 10000
	round.Distributable = rewardTotal - round.PoolFee
	if round.Distributable < 0 {
		round.Distributable = 0
	}
	round.Payouts = calculatePayouts(round.Workers, round.Distributable, round.AcceptedWork)
}

func calculatePayouts(workers []RoundWorker, distributable int64, totalWork float64) []PayoutEntry {
	if distributable <= 0 || totalWork <= 0 || len(workers) == 0 {
		return nil
	}
	payouts := make([]PayoutEntry, 0, len(workers))
	remainder := distributable
	for _, worker := range workers {
		share := int64(math.Floor((worker.AcceptedWork / totalWork) * float64(distributable)))
		if share < 0 {
			share = 0
		}
		payouts = append(payouts, PayoutEntry{
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

func (s *Service) pendingPayoutsLocked() []PayoutEntry {
	totals := make(map[string]*PayoutEntry)
	for _, round := range s.recentRounds {
		if !round.Solved || round.Paid {
			continue
		}
		for _, payout := range round.Payouts {
			entry, ok := totals[payout.Worker]
			if !ok {
				entry = &PayoutEntry{Worker: payout.Worker}
				totals[payout.Worker] = entry
			}
			entry.Amount += payout.Amount
			entry.Work += payout.Work
		}
	}
	payouts := make([]PayoutEntry, 0, len(totals))
	for _, payout := range totals {
		payouts = append(payouts, *payout)
	}
	sort.Slice(payouts, func(i, j int) bool {
		if payouts[i].Amount != payouts[j].Amount {
			return payouts[i].Amount > payouts[j].Amount
		}
		return payouts[i].Worker < payouts[j].Worker
	})
	return payouts
}

func (s *Service) balanceEntriesLocked() []BalanceEntry {
	type totals struct {
		unpaid int64
		paid   int64
	}
	entries := make(map[string]*totals)
	for _, round := range s.recentRounds {
		for _, payout := range round.Payouts {
			entry, ok := entries[payout.Worker]
			if !ok {
				entry = &totals{}
				entries[payout.Worker] = entry
			}
			if round.Paid {
				entry.paid += payout.Amount
			} else {
				entry.unpaid += payout.Amount
			}
		}
	}
	balances := make([]BalanceEntry, 0, len(entries))
	for worker, entry := range entries {
		balances = append(balances, BalanceEntry{
			Worker: worker,
			Unpaid: entry.unpaid,
			Paid:   entry.paid,
			Total:  entry.unpaid + entry.paid,
		})
	}
	sort.Slice(balances, func(i, j int) bool {
		if balances[i].Unpaid != balances[j].Unpaid {
			return balances[i].Unpaid > balances[j].Unpaid
		}
		if balances[i].Total != balances[j].Total {
			return balances[i].Total > balances[j].Total
		}
		return balances[i].Worker < balances[j].Worker
	})
	return balances
}

func (s *Service) ExecutePayouts(txid string, note string) (PaymentRecord, bool) {
	s.mu.Lock()

	payouts := s.pendingPayoutsLocked()
	if len(payouts) == 0 {
		s.mu.Unlock()
		return PaymentRecord{}, false
	}
	now := s.now().UTC()
	record := PaymentRecord{
		ID:        fmt.Sprintf("pay-%d", now.UnixNano()),
		CreatedAt: now,
		TxID:      strings.TrimSpace(txid),
		Note:      strings.TrimSpace(note),
		Payouts:   append([]PayoutEntry(nil), payouts...),
	}
	for _, payout := range payouts {
		record.Total += payout.Amount
	}
	for i := range s.recentRounds {
		if !s.recentRounds[i].Solved || s.recentRounds[i].Paid {
			continue
		}
		s.recentRounds[i].Paid = true
		record.Rounds = append(record.Rounds, s.recentRounds[i].ID)
	}
	s.state.Pool.Payments = append([]PaymentRecord{record}, s.state.Pool.Payments...)
	if len(s.state.Pool.Payments) > 50 {
		s.state.Pool.Payments = s.state.Pool.Payments[:50]
	}
	s.state.Pool.RecentRounds = cloneRounds(s.recentRounds)
	s.state.Pool.PendingPayouts = s.pendingPayoutsLocked()
	s.state.Pool.Balances = s.balanceEntriesLocked()
	snapshot := s.shareSnapshotLocked()
	s.mu.Unlock()
	s.persistShareUpdate(ShareEvent{
		Timestamp: now,
		Accepted:  true,
		Reason:    "payout executed",
	}, snapshot)
	return record, true
}

func adjustDifficulty(current float64, elapsed time.Duration, target time.Duration, minDiff float64, maxDiff float64) float64 {
	if current <= 0 {
		current = minDiff
	}
	if target <= 0 {
		return current
	}
	next := current
	if elapsed < target/2 {
		next = current * 2
	} else if elapsed > target*2 {
		next = current / 2
	}
	if next < minDiff {
		next = minDiff
	}
	if next > maxDiff {
		next = maxDiff
	}
	return next
}
