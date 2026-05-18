package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Pingancoin/pacpool/internal/service"
	"github.com/Pingancoin/pacpool/internal/upstream"
)

type fakePACD struct {
	mining   upstream.MiningInfo
	network  upstream.NetworkInfo
	template upstream.BlockTemplate
	err      error
}

func (f fakePACD) MiningInfo(context.Context) (upstream.MiningInfo, error) {
	return f.mining, f.err
}

func (f fakePACD) NetworkInfo(context.Context) (upstream.NetworkInfo, error) {
	return f.network, f.err
}

func (f fakePACD) BlockTemplate(context.Context, string) (upstream.BlockTemplate, error) {
	return f.template, f.err
}

func (f fakePACD) SubmitBlock(context.Context, string) (bool, uint32, string, error) {
	return true, f.template.Height, f.template.PreviousBlockHash, f.err
}

type fakePACData struct {
	status upstream.IndexStatus
	err    error
}

func (f fakePACData) Status(context.Context) (upstream.IndexStatus, error) {
	return f.status, f.err
}

func TestServiceSnapshotHealthy(t *testing.T) {
	svc, err := service.New(
		fakePACD{
			mining:  upstream.MiningInfo{Network: "simnet", Blocks: 15, NextHeight: 16},
			network: upstream.NetworkInfo{Network: "simnet", BestHeight: 15, BestBlockHash: "best"},
			template: upstream.BlockTemplate{
				Height:            16,
				PreviousBlockHash: "best",
				TransactionIDs:    []string{"tx1"},
			},
		},
		fakePACData{
			status: upstream.IndexStatus{Network: "simnet", IndexedHeight: 15, IndexedHash: "best"},
		},
		service.Options{
			Interval:   time.Second,
			FeeBPS:     500,
			MiningAddr: "SminingAddr",
			ShareDiff:  1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	svc.Refresh(context.Background())
	snapshot := svc.Snapshot()
	if !snapshot.Healthy || snapshot.Pool.FeePercent != 5 || !snapshot.Pool.TemplateBackfill || !snapshot.Pool.ReadyForStratum || !snapshot.Pool.Template.Available {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestServiceSnapshotUnhealthy(t *testing.T) {
	svc, err := service.New(
		fakePACD{err: errors.New("pacd down")},
		fakePACData{err: errors.New("pacdata down")},
		service.Options{
			Interval:  time.Second,
			FeeBPS:    500,
			ShareDiff: 1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	svc.Refresh(context.Background())
	snapshot := svc.Snapshot()
	if snapshot.Healthy || len(snapshot.Errors) == 0 {
		t.Fatalf("unexpected unhealthy snapshot: %+v", snapshot)
	}
}

func TestSetStratumStatsPersistsAcrossRefresh(t *testing.T) {
	svc, err := service.New(
		fakePACD{
			mining:  upstream.MiningInfo{Network: "simnet", Blocks: 15, NextHeight: 16},
			network: upstream.NetworkInfo{Network: "simnet", BestHeight: 15, BestBlockHash: "best"},
			template: upstream.BlockTemplate{
				Height:            16,
				PreviousBlockHash: "best",
				TransactionIDs:    []string{"tx1"},
			},
		},
		fakePACData{
			status: upstream.IndexStatus{Network: "simnet", IndexedHeight: 15, IndexedHash: "best"},
		},
		service.Options{
			Interval:      time.Second,
			FeeBPS:        500,
			MiningAddr:    "SminingAddr",
			ShareDiff:     1,
			VarDiff:       true,
			VarDiffTarget: 15 * time.Second,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	svc.SetStratumStats(3, 1)
	svc.Refresh(context.Background())
	snapshot := svc.Snapshot()
	if snapshot.Pool.ConnectedMiners != 3 || snapshot.Pool.ActiveJobs != 1 {
		t.Fatalf("unexpected stratum stats after refresh: %+v", snapshot.Pool)
	}
}

func TestRecordShareUpdatesPoolAndWorkers(t *testing.T) {
	base := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	current := base
	svc, err := service.New(fakePACD{}, fakePACData{}, service.Options{
		Interval:      time.Second,
		FeeBPS:        500,
		MiningAddr:    "SminingAddr",
		ShareDiff:     2.5,
		VarDiff:       true,
		VarDiffTarget: 15 * time.Second,
		VarDiffMin:    2.5,
		VarDiffMax:    10,
		Now: func() time.Time {
			return current
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	svc.RecordShare("miner.a", true, false, "")
	current = current.Add(3 * time.Second)
	svc.RecordShare("miner.a", false, false, "low difficulty share")
	current = current.Add(3 * time.Second)
	svc.RecordShare("miner.b", true, true, "")
	current = current.Add(1 * time.Second)
	svc.RecordShare("miner.a", true, false, "")

	snapshot := svc.Snapshot()
	if snapshot.Pool.ShareDifficulty != 2.5 {
		t.Fatalf("share difficulty = %v, want 2.5", snapshot.Pool.ShareDifficulty)
	}
	if snapshot.Pool.Shares.Accepted != 3 || snapshot.Pool.Shares.Rejected != 1 || snapshot.Pool.Shares.SolvedBlocks != 1 {
		t.Fatalf("unexpected share totals: %+v", snapshot.Pool.Shares)
	}
	if len(snapshot.Pool.Workers) != 2 {
		t.Fatalf("worker count = %d, want 2", len(snapshot.Pool.Workers))
	}
	if snapshot.Pool.Workers[0].Name != "miner.b" || snapshot.Pool.Workers[0].SolvedBlocks != 1 {
		t.Fatalf("unexpected top worker: %+v", snapshot.Pool.Workers[0])
	}
	if snapshot.Pool.Workers[1].Name != "miner.a" || snapshot.Pool.Workers[1].Rejected != 1 || snapshot.Pool.Workers[1].Difficulty != 5 || snapshot.Pool.Workers[1].LastError != "" {
		t.Fatalf("unexpected second worker: %+v", snapshot.Pool.Workers[1])
	}
}

func TestSharePersistenceReloadsState(t *testing.T) {
	dataDir := t.TempDir()
	base := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	current := base
	svc, err := service.New(fakePACD{}, fakePACData{}, service.Options{
		Interval:   time.Second,
		FeeBPS:     500,
		MiningAddr: "SminingAddr",
		ShareDiff:  1,
		DataDir:    dataDir,
		Now: func() time.Time {
			return current
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.RecordShare("miner.persist", true, false, "")
	current = current.Add(20 * time.Second)
	svc.RecordShare("miner.persist", false, false, "low difficulty share")

	reloaded, err := service.New(fakePACD{}, fakePACData{}, service.Options{
		Interval:   time.Second,
		FeeBPS:     500,
		MiningAddr: "SminingAddr",
		ShareDiff:  1,
		DataDir:    dataDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := reloaded.Snapshot()
	if snapshot.Pool.Shares.Accepted != 1 || snapshot.Pool.Shares.Rejected != 1 {
		t.Fatalf("unexpected reloaded share totals: %+v", snapshot.Pool.Shares)
	}
	if len(snapshot.Pool.Workers) != 1 || snapshot.Pool.Workers[0].Name != "miner.persist" {
		t.Fatalf("unexpected reloaded workers: %+v", snapshot.Pool.Workers)
	}
	if snapshot.Pool.LedgerPath == "" {
		t.Fatal("expected ledger path to be exposed")
	}
}
