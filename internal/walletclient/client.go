package walletclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Pingancoin/pacpool/internal/service"
)

const coin = int64(100_000_000)

type Client struct {
	baseURL    string
	token      string
	passphrase string
	fee        string
	httpClient *http.Client
}

type Options struct {
	BaseURL    string
	Token      string
	Passphrase string
	Fee        string
	Timeout    time.Duration
}

func New(opts Options) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("wallet URL is required")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	return &Client{
		baseURL:    baseURL,
		token:      strings.TrimSpace(opts.Token),
		passphrase: opts.Passphrase,
		fee:        strings.TrimSpace(opts.Fee),
		httpClient: &http.Client{Timeout: opts.Timeout},
	}, nil
}

func (c *Client) SendPayouts(ctx context.Context, payouts []service.PayoutEntry) (string, error) {
	if len(payouts) == 0 {
		return "", fmt.Errorf("no payouts to send")
	}
	payments := make([]paymentRequest, 0, len(payouts))
	for _, payout := range payouts {
		if payout.Amount <= 0 {
			continue
		}
		address := workerPayoutAddress(payout.Worker)
		if address == "" {
			return "", fmt.Errorf("worker %q does not include a payout address", payout.Worker)
		}
		payments = append(payments, paymentRequest{
			To:     address,
			Amount: formatPAC(payout.Amount),
		})
	}
	if len(payments) == 0 {
		return "", fmt.Errorf("no positive payouts to send")
	}
	body, err := json.Marshal(sendManyRequest{
		Payments:   payments,
		Fee:        c.fee,
		Passphrase: c.passphrase,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/sendmany", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("X-PACWallet-Token", c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errBody); err == nil && errBody.Error != "" {
			return "", fmt.Errorf("wallet sendmany returned %s: %s", resp.Status, errBody.Error)
		}
		return "", fmt.Errorf("wallet sendmany returned %s", resp.Status)
	}
	var result struct {
		Accepted bool   `json:"accepted"`
		TxID     string `json:"txid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if !result.Accepted {
		return "", fmt.Errorf("wallet rejected payout transaction %q", result.TxID)
	}
	if strings.TrimSpace(result.TxID) == "" {
		return "", fmt.Errorf("wallet sendmany response did not include txid")
	}
	return result.TxID, nil
}

func (c *Client) SpendableBalance(ctx context.Context) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/overview", nil)
	if err != nil {
		return 0, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("X-PACWallet-Token", c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("wallet overview returned %s", resp.Status)
	}
	var result struct {
		Balance struct {
			Spendable int64 `json:"spendable"`
		} `json:"balance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	if result.Balance.Spendable < 0 {
		return 0, fmt.Errorf("wallet overview returned negative spendable balance")
	}
	return result.Balance.Spendable, nil
}

type sendManyRequest struct {
	Payments   []paymentRequest `json:"payments"`
	Fee        string           `json:"fee,omitempty"`
	Passphrase string           `json:"passphrase,omitempty"`
}

type paymentRequest struct {
	To     string `json:"to"`
	Amount string `json:"amount"`
}

func workerPayoutAddress(worker string) string {
	worker = strings.TrimSpace(worker)
	if worker == "" {
		return ""
	}
	if beforeDot, _, ok := strings.Cut(worker, "."); ok {
		worker = beforeDot
	}
	return strings.TrimSpace(worker)
}

func formatPAC(atoms int64) string {
	sign := ""
	if atoms < 0 {
		sign = "-"
		atoms = -atoms
	}
	return fmt.Sprintf("%s%d.%08d", sign, atoms/coin, atoms%coin)
}
