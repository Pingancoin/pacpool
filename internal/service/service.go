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
}

type PACDataSource interface {
	Status(context.Context) (upstream.IndexStatus, error)
}

type Service struct {
	pacd     PACDSource
	pacdata  PACDataSource
	interval time.Duration
	feeBPS   int

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
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	FeePercent       float64  `json:"fee_percent"`
	ConnectedMiners  int      `json:"connected_miners"`
	ActiveJobs       int      `json:"active_jobs"`
	ReadyForStratum  bool     `json:"ready_for_stratum"`
	TemplateBackfill bool     `json:"template_backfill"`
	Notes            []string `json:"notes"`
}

func New(pacd PACDSource, pacdata PACDataSource, interval time.Duration, feeBPS int) *Service {
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
			Notes: []string{
				"Phase 0 control plane is live.",
				"Next step is pacd mining work/template RPC plus miner session handling.",
			},
		},
		Errors: make(map[string]string),
	}
	return &Service{
		pacd:     pacd,
		pacdata:  pacdata,
		interval: interval,
		feeBPS:   feeBPS,
		state:    state,
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

	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Errors = make(map[string]string)
	s.state.UpdatedAt = time.Now().UTC()
	s.state.Healthy = miningErr == nil && networkErr == nil && indexErr == nil

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

	s.state.Pool.ConnectedMiners = 0
	s.state.Pool.ActiveJobs = 0
	s.state.Pool.TemplateBackfill = s.state.Network.BestHeight > 0 && s.state.PACData.IndexedHeight == s.state.Network.BestHeight
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
