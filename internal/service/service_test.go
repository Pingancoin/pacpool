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
	if snapshot.Pool.CurrentRound.AcceptedShares != 3 || snapshot.Pool.CurrentRound.AcceptedWork != 7.5 {
		t.Fatalf("unexpected current round: %+v", snapshot.Pool.CurrentRound)
	}
	if len(snapshot.Pool.CurrentRound.Workers) != 2 {
		t.Fatalf("unexpected current round workers: %+v", snapshot.Pool.CurrentRound.Workers)
	}
}

func TestSharePersistenceReloadsState(t *testing.T) {
	dataDir := t.TempDir()
	base := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	current := base
	pacd := fakePACD{
		template: upstream.BlockTemplate{
			Height:    50,
			TotalFees: 5,
		},
	}
	pacd.template.NextSubsidy.Miner = 80
	svc, err := service.New(pacd, fakePACData{}, service.Options{
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
	svc.Refresh(context.Background())
	svc.RecordShare("miner.persist", true, false, "")
	current = current.Add(20 * time.Second)
	svc.RecordShare("miner.persist", false, false, "low difficulty share")
	current = current.Add(2 * time.Second)
	svc.RecordSolvedBlock("miner.persist", 50, "persist50")

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
	if snapshot.Pool.CurrentRound.AcceptedShares != 0 {
		t.Fatalf("unexpected persisted current round: %+v", snapshot.Pool.CurrentRound)
	}
	if len(snapshot.Pool.RecentRounds) != 1 || snapshot.Pool.RecentRounds[0].BlockHash != "persist50" {
		t.Fatalf("unexpected persisted recent rounds: %+v", snapshot.Pool.RecentRounds)
	}
	if len(snapshot.Pool.PendingPayouts) != 1 || snapshot.Pool.PendingPayouts[0].Amount != 76 {
		t.Fatalf("unexpected persisted pending payouts: %+v", snapshot.Pool.PendingPayouts)
	}
	payment, ok := reloaded.ExecutePayouts("persist-tx", "persist note")
	if !ok || payment.Total != 76 {
		t.Fatalf("unexpected persisted payout execution: %+v ok=%v", payment, ok)
	}
	reloadedAgain, err := service.New(fakePACD{}, fakePACData{}, service.Options{
		Interval:   time.Second,
		FeeBPS:     500,
		MiningAddr: "SminingAddr",
		ShareDiff:  1,
		DataDir:    dataDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	finalSnapshot := reloadedAgain.Snapshot()
	if len(finalSnapshot.Pool.Payments) != 1 || finalSnapshot.Pool.Payments[0].TxID != "persist-tx" {
		t.Fatalf("unexpected persisted payments: %+v", finalSnapshot.Pool.Payments)
	}
	if len(finalSnapshot.Pool.Balances) != 1 || finalSnapshot.Pool.Balances[0].Paid != 76 || finalSnapshot.Pool.Balances[0].Unpaid != 0 {
		t.Fatalf("unexpected persisted balances: %+v", finalSnapshot.Pool.Balances)
	}
}

func TestSolvedBlockClosesRoundAndStartsNext(t *testing.T) {
	base := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	current := base
	pacd := fakePACD{
		template: upstream.BlockTemplate{
			Height:    123,
			TotalFees: 10,
		},
	}
	pacd.template.NextSubsidy.Miner = 100
	svc, err := service.New(pacd, fakePACData{}, service.Options{
		Interval:   time.Second,
		FeeBPS:     500,
		MiningAddr: "SminingAddr",
		ShareDiff:  1,
		Now: func() time.Time {
			return current
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.Refresh(context.Background())
	svc.RecordShare("miner.round", true, true, "")
	current = current.Add(2 * time.Second)
	svc.RecordSolvedBlock("miner.round", 123, "abc123")

	snapshot := svc.Snapshot()
	if len(snapshot.Pool.RecentRounds) != 1 {
		t.Fatalf("recent rounds = %d, want 1", len(snapshot.Pool.RecentRounds))
	}
	round := snapshot.Pool.RecentRounds[0]
	if !round.Solved || round.FoundBy != "miner.round" || round.BlockHeight != 123 || round.BlockHash != "abc123" {
		t.Fatalf("unexpected archived round: %+v", round)
	}
	if round.RewardTotal != 100 || round.RewardFees != 10 || round.PoolFee != 5 || round.Distributable != 95 {
		t.Fatalf("unexpected round reward breakdown: %+v", round)
	}
	if len(round.Payouts) != 1 || round.Payouts[0].Worker != "miner.round" || round.Payouts[0].Amount != 95 {
		t.Fatalf("unexpected round payouts: %+v", round.Payouts)
	}
	if len(snapshot.Pool.PendingPayouts) != 1 || snapshot.Pool.PendingPayouts[0].Amount != 95 {
		t.Fatalf("unexpected pending payouts: %+v", snapshot.Pool.PendingPayouts)
	}
	if snapshot.Pool.CurrentRound.ID != round.ID+1 || snapshot.Pool.CurrentRound.AcceptedShares != 0 {
		t.Fatalf("unexpected next round state: %+v", snapshot.Pool.CurrentRound)
	}
}
