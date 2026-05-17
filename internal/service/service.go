package service

import (
	"context"
	"sync"
	"time"

	"github.com/Pingancoin/pacpool/internal/upstream"
)

type PACDSource interface {
	MiningInfo(context.Context) (upstream.MiningInfo, error)
	NetworkInfo(context.Context) (upstream.NetworkInfo, error)
	BlockTemplate(context.Context, string) (upstream.BlockTemplate, error)
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

	mu    sync.RWMutex
	state State
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
	ConnectedMiners  int           `json:"connected_miners"`
	ActiveJobs       int           `json:"active_jobs"`
	ReadyForStratum  bool          `json:"ready_for_stratum"`
	TemplateBackfill bool          `json:"template_backfill"`
	MiningAddress    string        `json:"mining_address,omitempty"`
	Template         TemplateState `json:"template"`
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

func New(pacd PACDSource, pacdata PACDataSource, interval time.Duration, feeBPS int, miningAddr string) *Service {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	state := State{
		Pool: PoolState{
			Name:             "pacpool",
			Version:          "0.1.0",
			FeePercent:       float64(feeBPS) / 100,
			ReadyForStratum:  false,
			TemplateBackfill: false,
			MiningAddress:    miningAddr,
			Notes: []string{
				"Phase 0 control plane is live.",
				"Template RPC is wired; next step is miner sessions, job broadcast, and share validation.",
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
		state:      state,
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
			s.state.Pool.Template = TemplateState{}
		}
	}

	s.state.Pool.ConnectedMiners = 0
	s.state.Pool.ActiveJobs = 0
	s.state.Pool.TemplateBackfill = s.state.Network.BestHeight > 0 && s.state.PACData.IndexedHeight == s.state.Network.BestHeight
	s.state.Pool.ReadyForStratum = s.miningAddr != "" && s.state.Pool.Template.Available && s.state.Pool.TemplateBackfill
}

func (s *Service) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clone := s.state
	clone.Pool.Notes = append([]string(nil), s.state.Pool.Notes...)
	if len(s.state.Errors) > 0 {
		clone.Errors = make(map[string]string, len(s.state.Errors))
		for k, v := range s.state.Errors {
			clone.Errors[k] = v
		}
	}
	return clone
}
