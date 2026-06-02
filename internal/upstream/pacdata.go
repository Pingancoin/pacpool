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

type PageInfo struct {
	Page       int  `json:"page"`
	Limit      int  `json:"limit"`
	Total      int  `json:"total"`
	TotalPages int  `json:"total_pages"`
	HasPrev    bool `json:"has_prev"`
	HasNext    bool `json:"has_next"`
}

type BlockList struct {
	PageInfo
	Entries []IndexedBlock `json:"entries"`
}

type IndexedBlock struct {
	Height     uint32   `json:"height"`
	Hash       string   `json:"hash"`
	PrevHash   string   `json:"prevhash"`
	Bits       string   `json:"bits"`
	Nonce      uint32   `json:"nonce"`
	Time       int64    `json:"time"`
	Difficulty string   `json:"difficulty,omitempty"`
	Subsidy    int64    `json:"subsidy,omitempty"`
	TxIDs      []string `json:"txids"`
}

type IndexedTx struct {
	Hash      string  `json:"hash"`
	Height    uint32  `json:"height"`
	BlockHash string  `json:"block_hash"`
	Time      int64   `json:"time"`
	Coinbase  bool    `json:"coinbase"`
	Vout      []TxOut `json:"vout"`
}

type TxOut struct {
	N        uint32 `json:"n"`
	Value    int64  `json:"value"`
	PkScript string `json:"pkscript"`
	Address  string `json:"address,omitempty"`
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
	err := c.get(ctx, "/status", &result)
	return result, err
}

func (c *PACDataClient) Blocks(ctx context.Context, page int, limit int) (BlockList, error) {
	var result BlockList
	err := c.get(ctx, fmt.Sprintf("/blocks?page=%d&limit=%d", page, limit), &result)
	return result, err
}

func (c *PACDataClient) Transaction(ctx context.Context, txid string) (IndexedTx, error) {
	var result IndexedTx
	err := c.get(ctx, "/tx/"+strings.TrimSpace(txid), &result)
	return result, err
}

func (c *PACDataClient) get(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pacdata %s returned %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}
