package upstream

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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

type BlockTemplate struct {
	Network           string   `json:"network"`
	Height            uint32   `json:"height"`
	PreviousBlockHash string   `json:"previousblockhash"`
	Bits              string   `json:"bits"`
	Difficulty        string   `json:"difficulty"`
	Timestamp         int64    `json:"timestamp"`
	TargetSpacingSec  int64    `json:"targetspacingsec"`
	MempoolSize       int      `json:"mempoolsize"`
	TotalFees         int64    `json:"totalfees"`
	CoinbaseTxID      string   `json:"coinbasetxid"`
	TransactionIDs    []string `json:"transactionids"`
	HeaderHex         string   `json:"headerhex"`
	BlockHex          string   `json:"blockhex"`
	Mutable           []string `json:"mutable"`
	NextSubsidy       struct {
		Miner   int64 `json:"miner"`
		Project int64 `json:"project"`
		Total   int64 `json:"total"`
	} `json:"nextsubsidy"`
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

func (c *PACDClient) BlockTemplate(ctx context.Context, address string) (BlockTemplate, error) {
	var result BlockTemplate
	body, err := json.Marshal(map[string]string{"address": address})
	if err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/getblocktemplate", bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("pacd /getblocktemplate returned %s", resp.Status)
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

func (c *PACDClient) SubmitBlock(ctx context.Context, blockHex string) (bool, uint32, string, error) {
	body, err := json.Marshal(map[string]string{"blockhex": strings.TrimSpace(blockHex)})
	if err != nil {
		return false, 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/submitblock", bytes.NewReader(body))
	if err != nil {
		return false, 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return false, 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, 0, "", fmt.Errorf("pacd /submitblock returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var result struct {
		Accepted bool   `json:"accepted"`
		Height   uint32 `json:"height"`
		Hash     string `json:"hash"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, 0, "", err
	}
	return result.Accepted, result.Height, result.Hash, nil
}

func ValidateBlockHex(blockHex string) error {
	_, err := hex.DecodeString(strings.TrimSpace(blockHex))
	return err
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
