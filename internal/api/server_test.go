package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func (fakePACData) Blocks(context.Context, int, int) (upstream.BlockList, error) {
	return upstream.BlockList{
		PageInfo: upstream.PageInfo{Page: 1, Limit: 100, Total: 1, TotalPages: 1},
		Entries: []upstream.IndexedBlock{{
			Height: 20,
			Hash:   "best",
			Time:   1780416800,
			TxIDs:  []string{"coinbase20"},
		}},
	}, nil
}

func (fakePACData) Transaction(context.Context, string) (upstream.IndexedTx, error) {
	return upstream.IndexedTx{
		Hash:     "coinbase20",
		Height:   20,
		Coinbase: true,
		Vout: []upstream.TxOut{{
			N:       0,
			Value:   95,
			Address: "SminingAddr",
		}},
	}, nil
}

type fakePayoutSender struct {
	txid      string
	payouts   []service.PayoutEntry
	spendable int64
}

func (f *fakePayoutSender) SendPayouts(_ context.Context, payouts []service.PayoutEntry) (string, error) {
	f.payouts = append([]service.PayoutEntry(nil), payouts...)
	return f.txid, nil
}

func (f *fakePayoutSender) SpendableBalance(context.Context) (int64, error) {
	if f.spendable > 0 {
		return f.spendable, nil
	}
	return 1 << 60, nil
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
	svc.RecordShare("worker.1", true, false, "", 0)

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

func TestDashboardSupportsLanguages(t *testing.T) {
	svc, err := service.New(fakePACD{}, fakePACData{}, service.Options{
		Interval:   time.Second,
		FeeBPS:     500,
		MiningAddr: "SminingAddr",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.Refresh(context.Background())

	server := httptest.NewServer(api.New(svc).Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/?lang=zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("dashboard content type = %q, want html", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"Pingancoin 矿池", "简体中文", "日本語", "한국어", "矿工接入方式", "stratum.pingancoin.org:3333", "PYourWalletAddress.rig01"} {
		if !strings.Contains(text, want) {
			t.Fatalf("dashboard missing %q in %s", want, text)
		}
	}
}

func TestAdminSettingsRequiresTokenAndUpdatesPool(t *testing.T) {
	svc, err := service.New(fakePACD{}, fakePACData{}, service.Options{
		Interval:   time.Second,
		FeeBPS:     500,
		MiningAddr: "SminingAddr",
		PayoutMin:  500_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.New(svc, api.Options{AdminToken: "secret"}).Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/admin")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("admin without token returned %s", resp.Status)
	}

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := url.Values{}
	form.Set("auto_payout", "1")
	form.Set("fee_percent", "2.50")
	form.Set("payout_min_pac", "7.5")
	form.Set("announcement_zh_cn", "今日维护完成\n请使用新版矿机 <script>alert(1)</script>")
	form.Set("announcement_en", "Maintenance complete\nPlease use the new miner")
	form.Set("announcement_ja", "メンテナンス完了")
	form.Set("announcement_ko", "점검 완료")
	form.Set("token", "secret")
	reqBody := strings.NewReader(form.Encode())
	resp, err = client.Post(server.URL+"/admin/settings", "application/x-www-form-urlencoded", reqBody)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("admin update returned %s", resp.Status)
	}

	status := svc.Snapshot()
	if !status.Pool.AutoPayout.Enabled || status.Pool.FeePercent != 2.5 || status.Pool.AutoPayout.MinAmount != 750_000_000 {
		t.Fatalf("settings not applied: %+v", status.Pool)
	}
	if status.Pool.Announcement != "今日维护完成\n请使用新版矿机 <script>alert(1)</script>" {
		t.Fatalf("announcement not applied: %q", status.Pool.Announcement)
	}
	if status.Pool.Announcements.En != "Maintenance complete\nPlease use the new miner" ||
		status.Pool.Announcements.Ja != "メンテナンス完了" ||
		status.Pool.Announcements.Ko != "점검 완료" {
		t.Fatalf("localized announcements not applied: %+v", status.Pool.Announcements)
	}

	resp, err = http.Get(server.URL + "/?lang=zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "今日维护完成") || !strings.Contains(text, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("dashboard did not render escaped announcement: %s", text)
	}
	if strings.Contains(text, "<script>alert(1)</script>") {
		t.Fatalf("dashboard rendered announcement as raw script: %s", text)
	}

	resp, err = http.Get(server.URL + "/?lang=en")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	text = string(body)
	if !strings.Contains(text, "Maintenance complete") || strings.Contains(text, "今日维护完成") {
		t.Fatalf("dashboard did not render English announcement: %s", text)
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
	svc.RecordShare("worker.1", true, true, "", 0)
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

func TestPayoutExecuteRequiresAdminTokenWhenConfigured(t *testing.T) {
	pacdTemplate := upstream.BlockTemplate{Height: 21}
	pacdTemplate.NextSubsidy.Miner = 100
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
	svc.RecordShare("worker.1", true, true, "", 0)
	svc.RecordSolvedBlock("worker.1", 21, "block21")

	server := httptest.NewServer(api.New(svc, api.Options{AdminToken: "secret"}).Handler())
	defer server.Close()

	body := bytes.NewBufferString(`{"txid":"tx123"}`)
	resp, err := http.Post(server.URL+"/payouts/execute", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized execute returned %s", resp.Status)
	}

	reqBody := bytes.NewBufferString(`{"txid":"tx123"}`)
	req, err := http.NewRequest(http.MethodPost, server.URL+"/payouts/execute", reqBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorized execute returned %s", resp.Status)
	}
}

func TestPayoutExecuteRequiresTxID(t *testing.T) {
	pacdTemplate := upstream.BlockTemplate{Height: 21}
	pacdTemplate.NextSubsidy.Miner = 100
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
	svc.RecordShare("worker.1", true, true, "", 0)
	svc.RecordSolvedBlock("worker.1", 21, "block21")

	server := httptest.NewServer(api.New(svc).Handler())
	defer server.Close()

	body := bytes.NewBufferString(`{"note":"missing txid"}`)
	resp, err := http.Post(server.URL+"/payouts/execute", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing txid returned %s", resp.Status)
	}
}

func TestPayoutAutoExecutesWalletPayout(t *testing.T) {
	pacdTemplate := upstream.BlockTemplate{Height: 21}
	pacdTemplate.NextSubsidy.Miner = 100
	sender := &fakePayoutSender{txid: "wallet-tx"}
	svc, err := service.New(fakePACD{
		mining:   upstream.MiningInfo{Network: "simnet", Blocks: 20, NextHeight: 21},
		network:  upstream.NetworkInfo{Network: "simnet", BestHeight: 20, BestBlockHash: "best"},
		template: pacdTemplate,
	}, fakePACData{}, service.Options{
		Interval:          time.Second,
		FeeBPS:            500,
		MiningAddr:        "SminingAddr",
		ShareDiff:         1,
		AutoPayout:        true,
		PayoutMin:         50,
		PayoutWindowStart: "00:00",
		PayoutWindowEnd:   "00:00",
		PayoutSender:      sender,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.Refresh(context.Background())
	svc.RecordShare("worker.1", true, true, "", 0)
	svc.RecordSolvedBlock("worker.1", 21, "block21")

	server := httptest.NewServer(api.New(svc, api.Options{AdminToken: "secret"}).Handler())
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/payouts/auto", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("auto payout returned %s", resp.Status)
	}
	var result struct {
		Executed bool                  `json:"executed"`
		Payment  service.PaymentRecord `json:"payment"`
		Status   service.PoolState     `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !result.Executed || result.Payment.TxID != "wallet-tx" || len(sender.payouts) != 1 {
		t.Fatalf("unexpected auto payout result=%+v sent=%+v", result, sender.payouts)
	}
	if len(result.Status.PendingPayouts) != 0 {
		t.Fatalf("unexpected pending payouts after auto payout: %+v", result.Status.PendingPayouts)
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
