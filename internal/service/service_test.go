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
	mining  upstream.MiningInfo
	network upstream.NetworkInfo
	err     error
}

func (f fakePACD) MiningInfo(context.Context) (upstream.MiningInfo, error) {
	return f.mining, f.err
}

func (f fakePACD) NetworkInfo(context.Context) (upstream.NetworkInfo, error) {
	return f.network, f.err
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
		},
		fakePACData{
			status: upstream.IndexStatus{Network: "simnet", IndexedHeight: 15, IndexedHash: "best"},
		},
		time.Second,
		500,
	)

	svc.Refresh(context.Background())
	snapshot := svc.Snapshot()
	if !snapshot.Healthy || snapshot.Pool.FeePercent != 5 || !snapshot.Pool.TemplateBackfill {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestServiceSnapshotUnhealthy(t *testing.T) {
	svc := service.New(
		fakePACD{err: errors.New("pacd down")},
		fakePACData{err: errors.New("pacdata down")},
		time.Second,
		500,
	)

	svc.Refresh(context.Background())
	snapshot := svc.Snapshot()
	if snapshot.Healthy || len(snapshot.Errors) == 0 {
		t.Fatalf("unexpected unhealthy snapshot: %+v", snapshot)
	}
}
