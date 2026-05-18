package service

import (
	"context"
	"sort"
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

	mu               sync.RWMutex
	state            State
	lastTemplate     upstream.BlockTemplate
	stratumConnected int
	stratumJobs      int
	workers          map[string]*WorkerState
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
	Name             string        `json:"name"`
	Version          string        `json:"version"`
	FeePercent       float64       `json:"fee_percent"`
	ShareDifficulty  float64       `json:"share_difficulty"`
	ConnectedMiners  int           `json:"connected_miners"`
	ActiveJobs       int           `json:"active_jobs"`
	ReadyForStratum  bool          `json:"ready_for_stratum"`
	TemplateBackfill bool          `json:"template_backfill"`
	MiningAddress    string        `json:"mining_address,omitempty"`
	Template         TemplateState `json:"template"`
	Shares           ShareState    `json:"shares"`
	Workers          []WorkerState `json:"workers,omitempty"`
	Notes            []string      `json:"notes"`
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
	Name         string    `json:"name"`
	Difficulty   float64   `json:"difficulty"`
	Accepted     uint64    `json:"accepted"`
	Rejected     uint64    `json:"rejected"`
	SolvedBlocks uint64    `json:"solved_blocks"`
	LastShareAt  time.Time `json:"last_share_at,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
}

func New(pacd PACDSource, pacdata PACDataSource, interval time.Duration, feeBPS int, miningAddr string, shareDiff float64) *Service {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if shareDiff <= 0 {
		shareDiff = 1
	}
	state := State{
		Pool: PoolState{
			Name:             "pacpool",
			Version:          "0.1.0",
			FeePercent:       float64(feeBPS) / 100,
			ShareDifficulty:  shareDiff,
			ReadyForStratum:  false,
			TemplateBackfill: false,
			MiningAddress:    miningAddr,
			Notes: []string{
				"Phase 0 control plane is live.",
				"Minimal Stratum work distribution is live.",
				"Per-worker share accounting is live.",
				"Next step is vardiff, payout logic, and miner dashboards.",
			},
		},
		Errors: make(map[string]string),
	}
	return &Service{
		pacd:       pacd,
		pacdata:    pacdata,
		interval:   interval,
		feeBPS:     feeBPS,
		miningAddr: miningAddr,
		shareDiff:  shareDiff,
		state:      state,
		workers:    make(map[string]*WorkerState),
	}
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
	s.state.UpdatedAt = time.Now().UTC()
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
	defer s.mu.Unlock()

	now := time.Now().UTC()
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
	ws.Difficulty = s.shareDiff
	if accepted {
		s.state.Pool.Shares.Accepted++
		s.state.Pool.Shares.LastAcceptedAt = now
		ws.Accepted++
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
	s.state.Pool.Workers = s.sortedWorkersLocked()
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
		if workers[i].Accepted != workers[j].Accepted {
			return workers[i].Accepted > workers[j].Accepted
		}
		return workers[i].Name < workers[j].Name
	})
	return workers
}
