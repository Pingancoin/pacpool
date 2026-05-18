package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ShareEvent struct {
	Timestamp  time.Time `json:"timestamp"`
	Worker     string    `json:"worker"`
	Difficulty float64   `json:"difficulty"`
	Accepted   bool      `json:"accepted"`
	Solved     bool      `json:"solved"`
	Reason     string    `json:"reason,omitempty"`
}

type shareSnapshot struct {
	Version          string        `json:"version"`
	UpdatedAt        time.Time     `json:"updated_at"`
	BaseDifficulty   float64       `json:"base_difficulty"`
	VarDiffEnabled   bool          `json:"vardiff_enabled"`
	VarDiffTargetSec int64         `json:"vardiff_target_sec"`
	Shares           ShareState    `json:"shares"`
	Workers          []WorkerState `json:"workers"`
}

func (s *Service) initPersistence() error {
	if strings.TrimSpace(s.dataDir) == "" {
		return nil
	}
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return err
	}
	s.ledgerPath = filepath.Join(s.dataDir, "share-events.jsonl")
	s.statePath = filepath.Join(s.dataDir, "share-state.json")
	s.state.Pool.LedgerPath = s.ledgerPath
	return s.loadShareState()
}

func (s *Service) loadShareState() error {
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var snapshot shareSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("decode share state: %w", err)
	}
	s.state.Pool.Shares = snapshot.Shares
	s.workers = make(map[string]*WorkerState, len(snapshot.Workers))
	for _, worker := range snapshot.Workers {
		workerCopy := worker
		if workerCopy.Difficulty <= 0 {
			workerCopy.Difficulty = s.shareDiff
		}
		s.workers[workerCopy.Name] = &workerCopy
	}
	s.state.Pool.Workers = s.sortedWorkersLocked()
	return nil
}

func (s *Service) shareSnapshotLocked() shareSnapshot {
	return shareSnapshot{
		Version:          s.state.Pool.Version,
		UpdatedAt:        s.now().UTC(),
		BaseDifficulty:   s.shareDiff,
		VarDiffEnabled:   s.varDiff,
		VarDiffTargetSec: int64(s.varTarget / time.Second),
		Shares:           s.state.Pool.Shares,
		Workers:          append([]WorkerState(nil), s.state.Pool.Workers...),
	}
}

func (s *Service) persistShareUpdate(event ShareEvent, snapshot shareSnapshot) {
	if s.ledgerPath == "" || s.statePath == "" {
		return
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	if err := s.appendShareEvent(event); err != nil {
		s.setLedgerError(err)
		return
	}
	if err := s.writeShareSnapshot(snapshot); err != nil {
		s.setLedgerError(err)
		return
	}
	s.setLedgerError(nil)
}

func (s *Service) appendShareEvent(event ShareEvent) error {
	file, err := os.OpenFile(s.ledgerPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = file.Write(encoded)
	return err
}

func (s *Service) writeShareSnapshot(snapshot shareSnapshot) error {
	encoded, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	tmpPath := s.statePath + ".tmp"
	if err := os.WriteFile(tmpPath, encoded, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.statePath)
}

func (s *Service) setLedgerError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.state.Pool.LastLedgerError = ""
		return
	}
	var buf bytes.Buffer
	buf.WriteString(err.Error())
	s.state.Pool.LastLedgerError = buf.String()
}
