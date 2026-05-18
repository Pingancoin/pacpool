package api_test

import (
	"bytes"
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

type fakePACD struct {
	mining   upstream.MiningInfo
	network  upstream.NetworkInfo
	template upstream.BlockTemplate
}

func (f fakePACD) NetworkInfo(context.Context) (upstream.NetworkInfo, error) {
	if f.network.Network != "" {
		return f.network, nil
	}
	return upstream.NetworkInfo{Network: "simnet", BestHeight: 20, BestBlockHash: "best"}, nil
}

func (f fakePACD) BlockTemplate(context.Context, string) (upstream.BlockTemplate, error) {
	if f.template.Height != 0 {
		return f.template, nil
	}
	return upstream.BlockTemplate{Height: 21, PreviousBlockHash: "best", TransactionIDs: []string{"tx1"}}, nil
}

func (f fakePACD) MiningInfo(context.Context) (upstream.MiningInfo, error) {
	if f.mining.Network != "" {
		return f.mining, nil
	}
	return upstream.MiningInfo{Network: "simnet", Blocks: 20, NextHeight: 21}, nil
}

func (f fakePACD) SubmitBlock(context.Context, string) (bool, uint32, string, error) {
	if f.template.Height != 0 {
		return true, f.template.Height, "best", nil
	}
	return true, 21, "best", nil
}

type fakePACData struct{}

func (fakePACData) Status(context.Context) (upstream.IndexStatus, error) {
	return upstream.IndexStatus{Network: "simnet", IndexedHeight: 20, IndexedHash: "best"}, nil
}

func TestServerStatusAndHealth(t *testing.T) {
	svc, err := service.New(fakePACD{}, fakePACData{}, service.Options{
		Interval:      time.Second,
		FeeBPS:        500,
		MiningAddr:    "SminingAddr",
		ShareDiff:     1.25,
		VarDiff:       true,
		VarDiffTarget: 15 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
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

func TestPayoutExecute(t *testing.T) {
	pacdTemplate := upstream.BlockTemplate{Height: 21}
	pacdTemplate.NextSubsidy.Miner = 100
	pacdTemplate.TotalFees = 10
	svc, err := service.New(fakePACD{
		mining:   upstream.MiningInfo{Network: "simnet", Blocks: 20, NextHeight: 21},
		network:  upstream.NetworkInfo{Network: "simnet", BestHeight: 20, BestBlockHash: "best"},
		template: pacdTemplate,
	}, fakePACData{}, service.Options{
		Interval:   time.Second,
		FeeBPS:     500,
		MiningAddr: "SminingAddr",
		ShareDiff:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.Refresh(context.Background())
	svc.RecordShare("worker.1", true, true, "")
	svc.RecordSolvedBlock("worker.1", 21, "block21")

	server := httptest.NewServer(api.New(svc).Handler())
	defer server.Close()

	body := bytes.NewBufferString(`{"txid":"tx123","note":"batch 1"}`)
	resp, err := http.Post(server.URL+"/payouts/execute", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("execute returned %s", resp.Status)
	}
	var result struct {
		Executed bool                  `json:"executed"`
		Payment  service.PaymentRecord `json:"payment"`
		Status   service.PoolState     `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !result.Executed || result.Payment.TxID != "tx123" || result.Payment.Total != 95 {
		t.Fatalf("unexpected payout execution result: %+v", result)
	}
	if len(result.Status.PendingPayouts) != 0 || len(result.Status.Payments) != 1 {
		t.Fatalf("unexpected payout status after execute: %+v", result.Status)
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
