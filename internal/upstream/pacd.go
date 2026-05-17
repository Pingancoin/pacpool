package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type PACDClient struct {
	baseURL string
	http    *http.Client
}

type MiningInfo struct {
	Network          string `json:"network"`
	Blocks           uint32 `json:"blocks"`
	BestBlockHash    string `json:"bestblockhash"`
	NextHeight       uint32 `json:"nextheight"`
	NextBits         string `json:"nextbits"`
	Difficulty       string `json:"difficulty"`
	TargetSpacingSec int64  `json:"targetspacingsec"`
	UTXOs            int    `json:"utxos"`
	Mempool          int    `json:"mempool"`
	DataFile         string `json:"datafile"`
	NextSubsidy      struct {
		Miner   int64 `json:"miner"`
		Project int64 `json:"project"`
		Total   int64 `json:"total"`
	} `json:"nextsubsidy"`
}

type NetworkInfo struct {
	Network          string `json:"network"`
	BestHeight       uint32 `json:"bestheight"`
	BestBlockHash    string `json:"bestblockhash"`
	MempoolSize      int    `json:"mempoolsize"`
	PeerCount        int    `json:"peercount"`
	KnownAddrCount   int    `json:"knownaddrcount"`
	TargetSpacingSec int64  `json:"targetspacingsec"`
}

func NewPACD(baseURL string) *PACDClient {
	return &PACDClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *PACDClient) MiningInfo(ctx context.Context) (MiningInfo, error) {
	var result MiningInfo
	err := c.get(ctx, "/getmininginfo", &result)
	return result, err
}

func (c *PACDClient) NetworkInfo(ctx context.Context) (NetworkInfo, error) {
	var result NetworkInfo
	err := c.get(ctx, "/getnetworkinfo", &result)
	return result, err
}

func (c *PACDClient) get(ctx context.Context, path string, dest any) error {
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
		return fmt.Errorf("pacd %s returned %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}
