package walletclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Pingancoin/pacpool/internal/service"
)

func TestSendPayoutsUsesSendMany(t *testing.T) {
	var got struct {
		Payments []struct {
			To     string `json:"to"`
			Amount string `json:"amount"`
		} `json:"payments"`
		Fee        string `json:"fee"`
		Passphrase string `json:"passphrase"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sendmany" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing auth header")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"accepted": true, "txid": "tx123"})
	}))
	defer server.Close()

	client, err := New(Options{
		BaseURL:    server.URL,
		Token:      "token",
		Passphrase: "secret",
		Fee:        "0.0001",
	})
	if err != nil {
		t.Fatal(err)
	}
	txid, err := client.SendPayouts(context.Background(), []service.PayoutEntry{{
		Worker: "Pabc.worker1",
		Amount: 123456789,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if txid != "tx123" {
		t.Fatalf("txid = %q", txid)
	}
	if len(got.Payments) != 1 || got.Payments[0].To != "Pabc" || got.Payments[0].Amount != "1.23456789" {
		t.Fatalf("unexpected sendmany body: %+v", got)
	}
	if got.Fee != "0.0001" || got.Passphrase != "secret" {
		t.Fatalf("unexpected fee/passphrase: %+v", got)
	}
}

func TestSpendableBalanceUsesOverview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/overview" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing auth header")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"balance": map[string]any{"spendable": int64(123456789)},
		})
	}))
	defer server.Close()

	client, err := New(Options{BaseURL: server.URL, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	spendable, err := client.SpendableBalance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if spendable != 123456789 {
		t.Fatalf("spendable = %d", spendable)
	}
}
