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
	svc := service.New(
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
		time.Second,
		500,
		"SminingAddr",
		1,
	)

	svc.Refresh(context.Background())
	snapshot := svc.Snapshot()
	if !snapshot.Healthy || snapshot.Pool.FeePercent != 5 || !snapshot.Pool.TemplateBackfill || !snapshot.Pool.ReadyForStratum || !snapshot.Pool.Template.Available {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestServiceSnapshotUnhealthy(t *testing.T) {
	svc := service.New(
		fakePACD{err: errors.New("pacd down")},
		fakePACData{err: errors.New("pacdata down")},
		time.Second,
		500,
		"",
		1,
	)

	svc.Refresh(context.Background())
	snapshot := svc.Snapshot()
	if snapshot.Healthy || len(snapshot.Errors) == 0 {
		t.Fatalf("unexpected unhealthy snapshot: %+v", snapshot)
	}
}

func TestSetStratumStatsPersistsAcrossRefresh(t *testing.T) {
	svc := service.New(
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
		time.Second,
		500,
		"SminingAddr",
		1,
	)

	svc.SetStratumStats(3, 1)
	svc.Refresh(context.Background())
	snapshot := svc.Snapshot()
	if snapshot.Pool.ConnectedMiners != 3 || snapshot.Pool.ActiveJobs != 1 {
		t.Fatalf("unexpected stratum stats after refresh: %+v", snapshot.Pool)
	}
}

func TestRecordShareUpdatesPoolAndWorkers(t *testing.T) {
	svc := service.New(fakePACD{}, fakePACData{}, time.Second, 500, "SminingAddr", 2.5)

	svc.RecordShare("miner.a", true, false, "")
	svc.RecordShare("miner.a", false, false, "low difficulty share")
	svc.RecordShare("miner.b", true, true, "")

	snapshot := svc.Snapshot()
	if snapshot.Pool.ShareDifficulty != 2.5 {
		t.Fatalf("share difficulty = %v, want 2.5", snapshot.Pool.ShareDifficulty)
	}
	if snapshot.Pool.Shares.Accepted != 2 || snapshot.Pool.Shares.Rejected != 1 || snapshot.Pool.Shares.SolvedBlocks != 1 {
		t.Fatalf("unexpected share totals: %+v", snapshot.Pool.Shares)
	}
	if len(snapshot.Pool.Workers) != 2 {
		t.Fatalf("worker count = %d, want 2", len(snapshot.Pool.Workers))
	}
	if snapshot.Pool.Workers[0].Name != "miner.b" || snapshot.Pool.Workers[0].SolvedBlocks != 1 {
		t.Fatalf("unexpected top worker: %+v", snapshot.Pool.Workers[0])
	}
	if snapshot.Pool.Workers[1].Name != "miner.a" || snapshot.Pool.Workers[1].Rejected != 1 || snapshot.Pool.Workers[1].LastError != "low difficulty share" {
		t.Fatalf("unexpected second worker: %+v", snapshot.Pool.Workers[1])
	}
}
