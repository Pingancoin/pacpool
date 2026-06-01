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

const coin = int64(100_000_000)

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
	pacd              PACDSource
	pacdata           PACDataSource
	payouts           PayoutSender
	interval          time.Duration
	feeBPS            int
	miningAddr        string
	shareDiff         float64
	varDiff           bool
	varTarget         time.Duration
	varMin            float64
	varMax            float64
	dataDir           string
	ledgerPath        string
	statePath         string
	settingsPath      string
	autoPayout        bool
	payoutMin         int64
	payoutEvery       time.Duration
	payoutWindowStart string
	payoutWindowEnd   string
	payoutLocation    *time.Location
	payoutBatchLimit  int
	now               func() time.Time

	mu               sync.RWMutex
	persistMu        sync.Mutex
	payoutMu         sync.Mutex
	state            State
	lastTemplate     upstream.BlockTemplate
	stratumConnected int
	stratumJobs      int
	workers          map[string]*WorkerState
	onlineWorkers    map[string]int
	nextRoundID      uint64
	currentRound     RoundState
	recentRounds     []RoundState
}

type PayoutSender interface {
	SendPayouts(context.Context, []PayoutEntry) (string, error)
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
	MiningOpen       bool            `json:"mining_open"`
	MiningStartTime  string          `json:"mining_start_time,omitempty"`
	MiningStartsIn   int64           `json:"mining_starts_in_sec,omitempty"`
	NotReadyReason   string          `json:"not_ready_reason,omitempty"`
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
	AutoPayout       AutoPayoutState `json:"auto_payout"`
	Notes            []string        `json:"notes"`
}

type AutoPayoutState struct {
	Enabled          bool      `json:"enabled"`
	WalletConfigured bool      `json:"wallet_configured"`
	MinAmount        int64     `json:"min_amount"`
	IntervalSec      int64     `json:"interval_sec"`
	WindowStart      string    `json:"window_start"`
	WindowEnd        string    `json:"window_end"`
	Timezone         string    `json:"timezone"`
	BatchLimit       int       `json:"batch_limit"`
	LastAttemptAt    time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt    time.Time `json:"last_success_at,omitempty"`
	LastTxID         string    `json:"last_txid,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
}

type TemplateState struct {
	Available         bool   `json:"available"`
	Height            uint32 `json:"height"`
	PreviousBlockHash string `json:"previousblockhash,omitempty"`
	Bits              string `json:"bits,omitempty"`
	Difficulty        string `json:"difficulty,omitempty"`
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
	Online         bool      `json:"online"`
	OnlineMachines int       `json:"online_machines,omitempty"`
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
	Paid   bool    `json:"paid,omitempty"`
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

type MinerStats struct {
	Address         string          `json:"address"`
	OnlineMachines  int             `json:"online_machines"`
	Workers         []WorkerState   `json:"workers"`
	Unpaid          int64           `json:"unpaid"`
	Paid            int64           `json:"paid"`
	Total           int64           `json:"total"`
	TodayEarned     int64           `json:"today_earned"`
	PendingPayouts  []PayoutEntry   `json:"pending_payouts,omitempty"`
	Payments        []PaymentRecord `json:"payments,omitempty"`
	LastPaymentAt   time.Time       `json:"last_payment_at,omitempty"`
	LastPaymentTxID string          `json:"last_payment_txid,omitempty"`
}

type Options struct {
	Interval          time.Duration
	FeeBPS            int
	MiningAddr        string
	ShareDiff         float64
	VarDiff           bool
	VarDiffTarget     time.Duration
	VarDiffMin        float64
	VarDiffMax        float64
	DataDir           string
	AutoPayout        bool
	PayoutMin         int64
	PayoutEvery       time.Duration
	PayoutWindowStart string
	PayoutWindowEnd   string
	PayoutTimezone    string
	PayoutBatchLimit  int
	PayoutSender      PayoutSender
	Now               func() time.Time
}

type AdminSettings struct {
	AutoPayoutEnabled bool    `json:"auto_payout_enabled"`
	FeeBPS            int     `json:"fee_bps"`
	FeePercent        float64 `json:"fee_percent"`
	PayoutMin         int64   `json:"payout_min"`
}

type AdminSettingsUpdate struct {
	AutoPayoutEnabled *bool
	FeeBPS            *int
	PayoutMin         *int64
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
	if opts.PayoutEvery <= 0 {
		opts.PayoutEvery = time.Hour
	}
	if opts.PayoutMin <= 0 {
		opts.PayoutMin = 5 * coin
	}
	if strings.TrimSpace(opts.PayoutWindowStart) == "" {
		opts.PayoutWindowStart = "00:00"
	}
	if strings.TrimSpace(opts.PayoutWindowEnd) == "" {
		opts.PayoutWindowEnd = "00:00"
	}
	if strings.TrimSpace(opts.PayoutTimezone) == "" {
		opts.PayoutTimezone = "Asia/Shanghai"
	}
	payoutLocation, err := time.LoadLocation(opts.PayoutTimezone)
	if err != nil {
		return nil, fmt.Errorf("payout timezone: %w", err)
	}
	if opts.PayoutBatchLimit <= 0 {
		opts.PayoutBatchLimit = 50
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
			AutoPayout: AutoPayoutState{
				Enabled:          opts.AutoPayout,
				WalletConfigured: opts.PayoutSender != nil,
				MinAmount:        opts.PayoutMin,
				IntervalSec:      int64(opts.PayoutEvery / time.Second),
				WindowStart:      opts.PayoutWindowStart,
				WindowEnd:        opts.PayoutWindowEnd,
				Timezone:         opts.PayoutTimezone,
				BatchLimit:       opts.PayoutBatchLimit,
			},
			Notes: []string{
				"Phase 0 control plane is live.",
				"Minimal Stratum work distribution is live.",
				"Per-worker share accounting is live.",
				"VarDiff, persistent share ledger, public dashboard, and payout execution are live.",
				"Wallet-linked automatic payout support is available when configured.",
			},
		},
		Errors: make(map[string]string),
	}
	svc := &Service{
		pacd:              pacd,
		pacdata:           pacdata,
		payouts:           opts.PayoutSender,
		interval:          opts.Interval,
		feeBPS:            opts.FeeBPS,
		miningAddr:        opts.MiningAddr,
		shareDiff:         opts.ShareDiff,
		varDiff:           opts.VarDiff,
		varTarget:         opts.VarDiffTarget,
		varMin:            opts.VarDiffMin,
		varMax:            opts.VarDiffMax,
		dataDir:           opts.DataDir,
		autoPayout:        opts.AutoPayout,
		payoutMin:         opts.PayoutMin,
		payoutEvery:       opts.PayoutEvery,
		payoutWindowStart: opts.PayoutWindowStart,
		payoutWindowEnd:   opts.PayoutWindowEnd,
		payoutLocation:    payoutLocation,
		payoutBatchLimit:  opts.PayoutBatchLimit,
		state:             state,
		workers:           make(map[string]*WorkerState),
		onlineWorkers:     make(map[string]int),
		now:               opts.Now,
		nextRoundID:       1,
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
	_, _, _ = s.TryAutoPayout(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	payoutTicker := time.NewTicker(s.payoutEvery)
	defer payoutTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.Refresh(ctx)
		case <-payoutTicker.C:
			_, _, _ = s.TryAutoPayout(ctx)
		}
	}
}

func (s *Service) TryAutoPayout(ctx context.Context) (PaymentRecord, bool, error) {
	if !s.autoPayout {
		return PaymentRecord{}, false, nil
	}
	if s.payouts == nil {
		err := fmt.Errorf("automatic payout wallet is not configured")
		s.setAutoPayoutError(err)
		return PaymentRecord{}, false, err
	}
	if !s.payoutMu.TryLock() {
		return PaymentRecord{}, false, nil
	}
	defer s.payoutMu.Unlock()

	if !s.inPayoutWindow(s.now()) {
		return PaymentRecord{}, false, nil
	}
	payouts, total := s.pendingPayoutBatch()
	if len(payouts) == 0 || total < s.payoutMin {
		return PaymentRecord{}, false, nil
	}
	s.setAutoPayoutAttempt("")
	txid, err := s.payouts.SendPayouts(ctx, payouts)
	if err != nil {
		s.setAutoPayoutError(err)
		return PaymentRecord{}, false, err
	}
	txid = strings.TrimSpace(txid)
	if txid == "" {
		err := fmt.Errorf("automatic payout wallet returned empty txid")
		s.setAutoPayoutError(err)
		return PaymentRecord{}, false, err
	}
	record, ok := s.ExecutePayoutBatch(txid, "automatic wallet payout", payouts)
	if !ok {
		err := fmt.Errorf("automatic payout sent tx %s but no pending payouts were available to mark paid", txid)
		s.setAutoPayoutError(err)
		return PaymentRecord{}, false, err
	}
	s.setAutoPayoutSuccess(txid)
	return record, true, nil
}

func (s *Service) pendingPayoutBatch() ([]PayoutEntry, int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := s.pendingPayoutsLocked()
	payouts := make([]PayoutEntry, 0, len(all))
	var total int64
	for _, payout := range all {
		if payout.Amount < s.payoutMin {
			continue
		}
		payouts = append(payouts, payout)
		total += payout.Amount
		if s.payoutBatchLimit > 0 && len(payouts) >= s.payoutBatchLimit {
			break
		}
	}
	return payouts, total
}

func (s *Service) Refresh(ctx context.Context) {
	mining, miningErr := s.pacd.MiningInfo(ctx)
	network, networkErr := s.pacd.NetworkInfo(ctx)
	index, indexErr := s.pacdata.Status(ctx)
	miningOpen := miningErr == nil && miningIsOpen(mining)
	var template upstream.BlockTemplate
	var templateErr error
	if s.miningAddr != "" && (miningErr != nil || miningOpen) {
		template, templateErr = s.pacd.BlockTemplate(ctx, s.miningAddr)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Errors = make(map[string]string)
	s.state.UpdatedAt = s.now().UTC()
	s.state.Healthy = miningErr == nil && networkErr == nil && indexErr == nil && (s.miningAddr == "" || !miningOpen || templateErr == nil)

	if miningErr == nil {
		s.state.PACD = mining
		s.state.Pool.MiningOpen = miningOpen
		s.state.Pool.MiningStartTime = mining.MiningStartTime
		s.state.Pool.MiningStartsIn = mining.TimeUntilMining
		s.state.Pool.NotReadyReason = ""
		if !miningOpen {
			s.state.Pool.NotReadyReason = "mining has not opened yet"
		}
	} else {
		s.state.Errors["pacd_mining"] = miningErr.Error()
		s.state.Pool.MiningOpen = false
		s.state.Pool.NotReadyReason = "pacd mining status is unavailable"
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
	if s.miningAddr != "" && miningOpen {
		if templateErr == nil {
			s.lastTemplate = template
			s.state.Pool.Template = TemplateState{
				Available:         true,
				Height:            template.Height,
				PreviousBlockHash: template.PreviousBlockHash,
				Bits:              template.Bits,
				Difficulty:        template.Difficulty,
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
	} else if s.miningAddr != "" && !miningOpen {
		s.lastTemplate = upstream.BlockTemplate{}
		s.state.Pool.Template = TemplateState{}
	}

	s.state.Pool.ConnectedMiners = s.stratumConnected
	s.state.Pool.ActiveJobs = s.stratumJobs
	s.state.Pool.TemplateBackfill = s.state.PACData.IndexedHeight == s.state.Network.BestHeight &&
		s.state.PACData.IndexedHash == s.state.Network.BestBlockHash
	s.state.Pool.ReadyForStratum = s.miningAddr != "" && miningOpen && s.state.Pool.Template.Available && s.state.Pool.TemplateBackfill
	if s.state.Pool.NotReadyReason == "" && !s.state.Pool.ReadyForStratum {
		switch {
		case s.miningAddr == "":
			s.state.Pool.NotReadyReason = "pool mining address is not configured"
		case !s.state.Pool.TemplateBackfill:
			s.state.Pool.NotReadyReason = "indexer is not caught up"
		case !s.state.Pool.Template.Available:
			s.state.Pool.NotReadyReason = "block template is unavailable"
		}
	}
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

func (s *Service) AdminSettings() AdminSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.adminSettingsLocked()
}

func (s *Service) UpdateAdminSettings(update AdminSettingsUpdate) (AdminSettings, error) {
	s.mu.Lock()
	if update.FeeBPS != nil {
		if *update.FeeBPS < 0 || *update.FeeBPS > 5000 {
			s.mu.Unlock()
			return AdminSettings{}, fmt.Errorf("fee bps must be between 0 and 5000")
		}
		s.feeBPS = *update.FeeBPS
		s.state.Pool.FeePercent = float64(s.feeBPS) / 100
	}
	if update.PayoutMin != nil {
		if *update.PayoutMin < 0 {
			s.mu.Unlock()
			return AdminSettings{}, fmt.Errorf("payout minimum cannot be negative")
		}
		s.payoutMin = *update.PayoutMin
		s.state.Pool.AutoPayout.MinAmount = s.payoutMin
		s.state.Pool.PendingPayouts = s.pendingPayoutsLocked()
		s.state.Pool.Balances = s.balanceEntriesLocked()
	}
	if update.AutoPayoutEnabled != nil {
		s.autoPayout = *update.AutoPayoutEnabled
		s.state.Pool.AutoPayout.Enabled = s.autoPayout
	}
	settings := s.adminSettingsLocked()
	s.mu.Unlock()

	if err := s.saveRuntimeSettings(settings); err != nil {
		return AdminSettings{}, err
	}
	return settings, nil
}

func (s *Service) adminSettingsLocked() AdminSettings {
	return AdminSettings{
		AutoPayoutEnabled: s.autoPayout,
		FeeBPS:            s.feeBPS,
		FeePercent:        float64(s.feeBPS) / 100,
		PayoutMin:         s.payoutMin,
	}
}

func (s *Service) MinerStats(address string) (MinerStats, bool) {
	address = strings.TrimSpace(address)
	if address == "" {
		return MinerStats{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := MinerStats{Address: address}
	nowLocal := s.now().In(s.payoutLocation)
	year, month, day := nowLocal.Date()
	todayStart := time.Date(year, month, day, 0, 0, 0, 0, s.payoutLocation)
	seenWorkers := make(map[string]struct{})
	for _, worker := range s.sortedWorkersLocked() {
		if payoutAddressFromWorker(worker.Name) != address {
			continue
		}
		stats.Workers = append(stats.Workers, worker)
		seenWorkers[worker.Name] = struct{}{}
	}
	for worker, count := range s.onlineWorkers {
		if payoutAddressFromWorker(worker) != address {
			continue
		}
		stats.OnlineMachines += count
		if _, ok := seenWorkers[worker]; !ok {
			stats.Workers = append(stats.Workers, WorkerState{
				Name:           worker,
				Difficulty:     s.shareDiff,
				Online:         count > 0,
				OnlineMachines: count,
			})
		}
	}
	sort.Slice(stats.Workers, func(i, j int) bool { return stats.Workers[i].Name < stats.Workers[j].Name })
	for _, round := range s.recentRounds {
		for _, payout := range round.Payouts {
			if payoutAddressFromWorker(payout.Worker) != address {
				continue
			}
			if round.Paid || payout.Paid {
				stats.Paid += payout.Amount
			} else {
				stats.Unpaid += payout.Amount
				stats.PendingPayouts = append(stats.PendingPayouts, payout)
			}
			if !round.EndedAt.IsZero() && !round.EndedAt.In(s.payoutLocation).Before(todayStart) {
				stats.TodayEarned += payout.Amount
			}
		}
	}
	stats.Total = stats.Paid + stats.Unpaid
	for _, payment := range s.state.Pool.Payments {
		var matched PaymentRecord
		for _, payout := range payment.Payouts {
			if payoutAddressFromWorker(payout.Worker) != address {
				continue
			}
			matched.ID = payment.ID
			matched.CreatedAt = payment.CreatedAt
			matched.TxID = payment.TxID
			matched.Note = payment.Note
			matched.Rounds = append([]uint64(nil), payment.Rounds...)
			matched.Payouts = append(matched.Payouts, payout)
			matched.Total += payout.Amount
		}
		if len(matched.Payouts) == 0 {
			continue
		}
		stats.Payments = append(stats.Payments, matched)
		if stats.LastPaymentAt.IsZero() || matched.CreatedAt.After(stats.LastPaymentAt) {
			stats.LastPaymentAt = matched.CreatedAt
			stats.LastPaymentTxID = matched.TxID
		}
	}
	return stats, len(stats.Workers) > 0 || stats.Total > 0 || len(stats.Payments) > 0
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

func (s *Service) SetStratumStats(connected int, jobs int, workerNames []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stratumConnected = connected
	s.stratumJobs = jobs
	s.onlineWorkers = make(map[string]int)
	for _, ws := range s.workers {
		ws.Online = false
		ws.OnlineMachines = 0
	}
	for _, worker := range workerNames {
		worker = strings.TrimSpace(worker)
		if worker == "" {
			continue
		}
		s.onlineWorkers[worker]++
		ws, ok := s.workers[worker]
		if !ok {
			ws = &WorkerState{
				Name:       worker,
				Difficulty: s.shareDiff,
			}
			s.workers[worker] = ws
		}
		ws.Online = true
		ws.OnlineMachines = s.onlineWorkers[worker]
	}
	s.state.Pool.ConnectedMiners = connected
	s.state.Pool.ActiveJobs = jobs
	s.state.Pool.Workers = s.sortedWorkersLocked()
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
		clone := *worker
		clone.OnlineMachines = s.onlineWorkers[clone.Name]
		clone.Online = clone.OnlineMachines > 0
		workers = append(workers, clone)
	}
	sort.Slice(workers, func(i, j int) bool {
		if workers[i].Online != workers[j].Online {
			return workers[i].Online
		}
		if workers[i].Difficulty != workers[j].Difficulty {
			return workers[i].Difficulty > workers[j].Difficulty
		}
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
			if payout.Paid {
				continue
			}
			address := payoutAddressFromWorker(payout.Worker)
			if address == "" {
				continue
			}
			entry, ok := totals[address]
			if !ok {
				entry = &PayoutEntry{Worker: address}
				totals[address] = entry
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
			address := payoutAddressFromWorker(payout.Worker)
			if address == "" {
				continue
			}
			entry, ok := entries[address]
			if !ok {
				entry = &totals{}
				entries[address] = entry
			}
			if round.Paid || payout.Paid {
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
	return s.executePayoutsLocked(txid, note, payouts)
}

func (s *Service) ExecutePayoutBatch(txid string, note string, payouts []PayoutEntry) (PaymentRecord, bool) {
	s.mu.Lock()
	if len(payouts) == 0 {
		s.mu.Unlock()
		return PaymentRecord{}, false
	}
	return s.executePayoutsLocked(txid, note, payouts)
}

func (s *Service) executePayoutsLocked(txid string, note string, payouts []PayoutEntry) (PaymentRecord, bool) {
	now := s.now().UTC()
	record := PaymentRecord{
		ID:        fmt.Sprintf("pay-%d", now.UnixNano()),
		CreatedAt: now,
		TxID:      strings.TrimSpace(txid),
		Note:      strings.TrimSpace(note),
		Payouts:   append([]PayoutEntry(nil), payouts...),
	}
	selected := make(map[string]struct{}, len(payouts))
	for _, payout := range payouts {
		record.Total += payout.Amount
		if address := payoutAddressFromWorker(payout.Worker); address != "" {
			selected[address] = struct{}{}
		}
	}
	touchedRounds := make(map[uint64]struct{})
	for i := range s.recentRounds {
		if !s.recentRounds[i].Solved || s.recentRounds[i].Paid {
			continue
		}
		for j := range s.recentRounds[i].Payouts {
			if s.recentRounds[i].Payouts[j].Paid {
				continue
			}
			address := payoutAddressFromWorker(s.recentRounds[i].Payouts[j].Worker)
			if _, ok := selected[address]; ok {
				s.recentRounds[i].Payouts[j].Paid = true
				touchedRounds[s.recentRounds[i].ID] = struct{}{}
			}
		}
		s.recentRounds[i].Paid = roundFullyPaid(s.recentRounds[i])
	}
	if len(touchedRounds) == 0 {
		s.mu.Unlock()
		return PaymentRecord{}, false
	}
	for roundID := range touchedRounds {
		record.Rounds = append(record.Rounds, roundID)
	}
	sort.Slice(record.Rounds, func(i, j int) bool { return record.Rounds[i] < record.Rounds[j] })
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

func roundFullyPaid(round RoundState) bool {
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

func (s *Service) inPayoutWindow(t time.Time) bool {
	start, ok := parseClock(s.payoutWindowStart)
	if !ok {
		return true
	}
	end, ok := parseClock(s.payoutWindowEnd)
	if !ok {
		return true
	}
	if start == end {
		return true
	}
	local := t.In(s.payoutLocation)
	minute := local.Hour()*60 + local.Minute()
	if start <= end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}

func parseClock(value string) (int, bool) {
	var hour, minute int
	if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d:%d", &hour, &minute); err != nil {
		return 0, false
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

func payoutAddressFromWorker(worker string) string {
	worker = strings.TrimSpace(worker)
	if worker == "" {
		return ""
	}
	if beforeDot, _, ok := strings.Cut(worker, "."); ok {
		return strings.TrimSpace(beforeDot)
	}
	return worker
}

func miningIsOpen(info upstream.MiningInfo) bool {
	return info.MiningOpen || (info.MiningStartTS == 0 && strings.TrimSpace(info.MiningStartTime) == "")
}

func (s *Service) setAutoPayoutAttempt(lastErr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Pool.AutoPayout.LastAttemptAt = s.now().UTC()
	s.state.Pool.AutoPayout.LastError = strings.TrimSpace(lastErr)
}

func (s *Service) setAutoPayoutError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Pool.AutoPayout.LastAttemptAt = s.now().UTC()
	s.state.Pool.AutoPayout.LastError = err.Error()
}

func (s *Service) setAutoPayoutSuccess(txid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Pool.AutoPayout.LastSuccessAt = s.now().UTC()
	s.state.Pool.AutoPayout.LastTxID = strings.TrimSpace(txid)
	s.state.Pool.AutoPayout.LastError = ""
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
