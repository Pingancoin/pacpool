package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type PACDataClient struct {
	baseURL string
	http    *http.Client
}

type IndexStatus struct {
	Network       string    `json:"network"`
	IndexedHeight uint32    `json:"indexed_height"`
	IndexedHash   string    `json:"indexed_hash"`
	IndexedAt     time.Time `json:"indexed_at"`
	BlockCount    int       `json:"blocks"`
	TxCount       int       `json:"transactions"`
	AddressCount  int       `json:"addresses"`
}

func NewPACData(baseURL string) *PACDataClient {
	return &PACDataClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *PACDataClient) Status(ctx context.Context) (IndexStatus, error) {
	var result IndexStatus
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/status", nil)
	if err != nil {
		return result, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("pacdata /status returned %s", resp.Status)
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}
