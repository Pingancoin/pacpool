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
	mining      upstream.MiningInfo
	network     upstream.NetworkInfo
	template    upstream.BlockTemplate
	err         error
	templateErr error
}

func (f fakePACD) MiningInfo(context.Context) (upstream.MiningInfo, error) {
	return f.mining, f.err
}

func (f fakePACD) NetworkInfo(context.Context) (upstream.NetworkInfo, error) {
	return f.network, f.err
}

func (f fakePACD) BlockTemplate(context.Context, string) (upstream.BlockTemplate, error) {
	if f.templateErr != nil {
		return upstream.BlockTemplate{}, f.templateErr
	}
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

type fakePayoutSender struct {
	txid    string
	payouts []service.PayoutEntry
	err     error
}

func (f *fakePayoutSender) SendPayouts(_ context.Context, payouts []service.PayoutEntry) (string, error) {
	f.payouts = append([]service.PayoutEntry(nil), payouts...)
	if f.err != nil {
		return "", f.err
	}
	return f.txid, nil
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

func TestServiceSnapshotReadyAtGenesisHeight(t *testing.T) {
	svc, err := service.New(
		fakePACD{
			mining:  upstream.MiningInfo{Network: "mainnet", Blocks: 0, BestBlockHash: "genesis", NextHeight: 1},
			network: upstream.NetworkInfo{Network: "mainnet", BestHeight: 0, BestBlockHash: "genesis"},
			template: upstream.BlockTemplate{
				Height:            1,
				PreviousBlockHash: "genesis",
				TransactionIDs:    []string{"coinbase"},
			},
		},
		fakePACData{
			status: upstream.IndexStatus{Network: "mainnet", IndexedHeight: 0, IndexedHash: "genesis"},
		},
		service.Options{
			Interval:   time.Second,
			FeeBPS:     500,
			MiningAddr: "PpoolAddress",
			ShareDiff:  1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	svc.Refresh(context.Background())
	snapshot := svc.Snapshot()
	if !snapshot.Pool.TemplateBackfill || !snapshot.Pool.ReadyForStratum {
		t.Fatalf("expected genesis-height chain to be stratum-ready: %+v", snapshot.Pool)
	}
}

func TestServiceSnapshotWaitsForMiningOpen(t *testing.T) {
	svc, err := service.New(
		fakePACD{
			mining: upstream.MiningInfo{
				Network:         "mainnet",
				Blocks:          0,
				BestBlockHash:   "genesis",
				NextHeight:      1,
				MiningOpen:      false,
				MiningStartTime: "2026-06-01T00:00:00Z",
				MiningStartTS:   1780272000,
				TimeUntilMining: 3600,
			},
			network:     upstream.NetworkInfo{Network: "mainnet", BestHeight: 0, BestBlockHash: "genesis"},
			templateErr: errors.New("template should not be requested before mining opens"),
		},
		fakePACData{
			status: upstream.IndexStatus{Network: "mainnet", IndexedHeight: 0, IndexedHash: "genesis"},
		},
		service.Options{
			Interval:   time.Second,
			FeeBPS:     500,
			MiningAddr: "PpoolAddress",
			ShareDiff:  1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	svc.Refresh(context.Background())
	snapshot := svc.Snapshot()
	if !snapshot.Healthy || snapshot.Pool.ReadyForStratum || snapshot.Pool.Template.Available || snapshot.Pool.MiningOpen {
		t.Fatalf("unexpected pre-launch snapshot: %+v", snapshot)
	}
	if snapshot.Pool.MiningStartTime == "" || snapshot.Pool.NotReadyReason == "" {
		t.Fatalf("missing launch wait state: %+v", snapshot.Pool)
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

	svc.SetStratumStats(3, 1, []string{"miner.a", "miner.a", "miner.b"})
	svc.Refresh(context.Background())
	snapshot := svc.Snapshot()
	if snapshot.Pool.ConnectedMiners != 3 || snapshot.Pool.ActiveJobs != 1 {
		t.Fatalf("unexpected stratum stats after refresh: %+v", snapshot.Pool)
	}
	svc.SetStratumStats(0, 1, nil)
	snapshot = svc.Snapshot()
	if snapshot.Pool.ConnectedMiners != 0 {
		t.Fatalf("expected no connected miners after disconnect: %+v", snapshot.Pool)
	}
	for _, worker := range snapshot.Pool.Workers {
		if worker.Online || worker.OnlineMachines != 0 {
			t.Fatalf("worker stayed online after disconnect: %+v", worker)
		}
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

func TestTryAutoPayoutSendsAndMarksPaid(t *testing.T) {
	base := time.Date(2026, 5, 18, 13, 0, 0, 0, time.UTC)
	current := base
	pacd := fakePACD{
		template: upstream.BlockTemplate{Height: 200},
	}
	pacd.template.NextSubsidy.Miner = 100
	sender := &fakePayoutSender{txid: "auto-tx"}
	svc, err := service.New(pacd, fakePACData{}, service.Options{
		Interval:          time.Second,
		FeeBPS:            500,
		MiningAddr:        "SminingAddr",
		ShareDiff:         1,
		AutoPayout:        true,
		PayoutMin:         50,
		PayoutWindowStart: "00:00",
		PayoutWindowEnd:   "23:59",
		PayoutSender:      sender,
		Now: func() time.Time {
			return current
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.Refresh(context.Background())
	svc.RecordShare("Pminer.worker1", true, true, "")
	current = current.Add(time.Second)
	svc.RecordSolvedBlock("Pminer.worker1", 200, "block200")

	record, ok, err := svc.TryAutoPayout(context.Background())
	if err != nil || !ok {
		t.Fatalf("auto payout failed: record=%+v ok=%v err=%v", record, ok, err)
	}
	if record.TxID != "auto-tx" || record.Total != 95 {
		t.Fatalf("unexpected automatic payout record: %+v", record)
	}
	if len(sender.payouts) != 1 || sender.payouts[0].Worker != "Pminer" || sender.payouts[0].Amount != 95 {
		t.Fatalf("unexpected wallet payouts: %+v", sender.payouts)
	}
	snapshot := svc.Snapshot()
	if len(snapshot.Pool.PendingPayouts) != 0 || len(snapshot.Pool.Payments) != 1 {
		t.Fatalf("unexpected post payout state: %+v", snapshot.Pool)
	}
	if snapshot.Pool.AutoPayout.LastTxID != "auto-tx" || snapshot.Pool.AutoPayout.LastError != "" {
		t.Fatalf("unexpected auto payout status: %+v", snapshot.Pool.AutoPayout)
	}
}

func TestEqualPayoutWindowMeansAllDay(t *testing.T) {
	current := time.Date(2026, 6, 1, 13, 30, 0, 0, time.UTC)
	pacd := fakePACD{template: upstream.BlockTemplate{Height: 201}}
	pacd.template.NextSubsidy.Miner = 100
	sender := &fakePayoutSender{txid: "all-day-tx"}
	svc, err := service.New(pacd, fakePACData{}, service.Options{
		Interval:          time.Second,
		FeeBPS:            500,
		MiningAddr:        "SminingAddr",
		ShareDiff:         1,
		AutoPayout:        true,
		PayoutMin:         50,
		PayoutWindowStart: "00:00",
		PayoutWindowEnd:   "00:00",
		PayoutSender:      sender,
		Now: func() time.Time {
			return current
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.Refresh(context.Background())
	svc.RecordShare("Pminer.worker1", true, true, "")
	current = current.Add(time.Second)
	svc.RecordSolvedBlock("Pminer.worker1", 201, "block201")

	record, ok, err := svc.TryAutoPayout(context.Background())
	if err != nil || !ok {
		t.Fatalf("auto payout should run during all-day window: record=%+v ok=%v err=%v", record, ok, err)
	}
	if record.TxID != "all-day-tx" {
		t.Fatalf("unexpected txid: %+v", record)
	}
}

func TestPendingPayoutsAggregateByPayoutAddress(t *testing.T) {
	base := time.Date(2026, 5, 18, 1, 0, 0, 0, time.UTC)
	current := base
	pacd := fakePACD{template: upstream.BlockTemplate{Height: 400}}
	pacd.template.NextSubsidy.Miner = 1_000_000_000
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
	svc.RecordShare("Pminer.rig1", true, false, "")
	svc.RecordShare("Pminer.rig2", true, true, "")
	current = current.Add(time.Second)
	svc.RecordSolvedBlock("Pminer.rig2", 400, "block400")

	snapshot := svc.Snapshot()
	if len(snapshot.Pool.PendingPayouts) != 1 {
		t.Fatalf("unexpected payout count: %+v", snapshot.Pool.PendingPayouts)
	}
	if snapshot.Pool.PendingPayouts[0].Worker != "Pminer" || snapshot.Pool.PendingPayouts[0].Amount != 950_000_000 {
		t.Fatalf("unexpected aggregated payout: %+v", snapshot.Pool.PendingPayouts[0])
	}
}

func TestMinerStatsUsesPayoutAddressAndOnlineWorkers(t *testing.T) {
	base := time.Date(2026, 5, 18, 0, 30, 0, 0, time.UTC)
	current := base
	pacd := fakePACD{template: upstream.BlockTemplate{Height: 300}}
	pacd.template.NextSubsidy.Miner = 1_000_000_000
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
	svc.RecordShare("Pminer.worker1", true, true, "")
	current = current.Add(time.Second)
	svc.RecordSolvedBlock("Pminer.worker1", 300, "block300")
	svc.SetStratumStats(2, 1, []string{"Pminer.worker1", "Pminer.worker2"})

	stats, found := svc.MinerStats("Pminer")
	if !found {
		t.Fatal("expected miner stats to be found")
	}
	if stats.OnlineMachines != 2 || len(stats.Workers) != 2 {
		t.Fatalf("unexpected online stats: %+v", stats)
	}
	if stats.Unpaid != 950_000_000 || stats.TodayEarned != 950_000_000 || stats.Total != 950_000_000 {
		t.Fatalf("unexpected miner balances: %+v", stats)
	}
}
