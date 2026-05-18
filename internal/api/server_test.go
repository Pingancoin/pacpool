package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Pingancoin/pacpool/internal/api"
	"github.com/Pingancoin/pacpool/internal/service"
	"github.com/Pingancoin/pacpool/internal/upstream"
)

type fakePACD struct{}

func (fakePACD) MiningInfo(context.Context) (upstream.MiningInfo, error) {
	return upstream.MiningInfo{Network: "simnet", Blocks: 20, NextHeight: 21}, nil
}

func (fakePACD) NetworkInfo(context.Context) (upstream.NetworkInfo, error) {
	return upstream.NetworkInfo{Network: "simnet", BestHeight: 20, BestBlockHash: "best"}, nil
}

func (fakePACD) BlockTemplate(context.Context, string) (upstream.BlockTemplate, error) {
	return upstream.BlockTemplate{Height: 21, PreviousBlockHash: "best", TransactionIDs: []string{"tx1"}}, nil
}

func (fakePACD) SubmitBlock(context.Context, string) (bool, uint32, string, error) {
	return true, 21, "best", nil
}

type fakePACData struct{}

func (fakePACData) Status(context.Context) (upstream.IndexStatus, error) {
	return upstream.IndexStatus{Network: "simnet", IndexedHeight: 20, IndexedHash: "best"}, nil
}

func TestServerStatusAndHealth(t *testing.T) {
	svc := service.New(fakePACD{}, fakePACData{}, time.Second, 500, "SminingAddr", 1.25)
	svc.Refresh(context.Background())
	svc.RecordShare("worker.1", true, false, "")

	server := httptest.NewServer(api.New(svc).Handler())
	defer server.Close()

	var status service.State
	getJSON(t, server.URL+"/status", &status)
	if !status.Healthy || status.Pool.Name != "pacpool" || status.Network.BestHeight != 20 {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.Pool.ShareDifficulty != 1.25 || status.Pool.Shares.Accepted != 1 || len(status.Pool.Workers) != 1 {
		t.Fatalf("unexpected pool share status: %+v", status.Pool)
	}

	resp, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health returned %s", resp.Status)
	}
}

func getJSON(t *testing.T, url string, dest any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s returned %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		t.Fatal(err)
	}
}
