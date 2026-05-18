package stratum

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Pingancoin/pacpool/internal/upstream"
	"github.com/decred/dcrd/crypto/blake256"
)

type fakeSvc struct {
	template    upstream.BlockTemplate
	connected   int
	activeJobs  int
	blockHex    string
	shareDiff   float64
	accepted    int
	rejected    int
	solved      int
	lastWorker  string
	lastReason  string
	blockHeight uint32
	blockHash   string
}

func (f *fakeSvc) CurrentTemplate() (upstream.BlockTemplate, bool) {
	return f.template, true
}

func (f *fakeSvc) SubmitSolvedBlock(_ context.Context, blockHex string) (bool, uint32, string, error) {
	f.blockHex = blockHex
	return true, f.template.Height, "blockhash", nil
}

func (f *fakeSvc) ShareDifficulty() float64 {
	if f.shareDiff <= 0 {
		return 1
	}
	return f.shareDiff
}

func (f *fakeSvc) WorkerDifficulty(string) float64 {
	return f.ShareDifficulty()
}

func (f *fakeSvc) SetStratumStats(connected int, jobs int) {
	f.connected = connected
	f.activeJobs = jobs
}

func (f *fakeSvc) RecordShare(worker string, accepted bool, solved bool, reason string) {
	f.lastWorker = worker
	f.lastReason = reason
	if accepted {
		f.accepted++
		if f.shareDiff > 0 {
			f.shareDiff *= 2
		}
	} else {
		f.rejected++
	}
	if solved {
		f.solved++
	}
}

func (f *fakeSvc) RecordSolvedBlock(worker string, height uint32, hash string) {
	f.lastWorker = worker
	f.blockHeight = height
	f.blockHash = hash
}

func TestSubscribeAuthorizeAndSubmit(t *testing.T) {
	header := make([]byte, headerLength)
	binary.LittleEndian.PutUint64(header[headerTimestampOffset:headerBitsOffset], 1)
	binary.LittleEndian.PutUint32(header[headerBitsOffset:headerNonceOffset], 0x207fffff)
	binary.LittleEndian.PutUint32(header[headerHeightOffset:headerLength], 16)
	block := append(append([]byte(nil), header...), 0x00)
	template := upstream.BlockTemplate{
		Height:            16,
		PreviousBlockHash: strings.Repeat("0", 64),
		Bits:              "207fffff",
		Timestamp:         1,
		CoinbaseTxID:      strings.Repeat("1", 64),
		HeaderHex:         hex.EncodeToString(header),
		BlockHex:          hex.EncodeToString(block),
	}
	provider := &fakeSvc{template: template, shareDiff: 1}
	addr := freeAddr(t)
	server := New(addr, provider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = server.Run(ctx)
	}()
	waitForTCP(t, addr)
	server.updateJob(template)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)

	writeLine(t, conn, `{"id":1,"method":"mining.subscribe","params":[]}`)
	var subscribe map[string]any
	readJSONLine(t, reader, &subscribe)
	if subscribe["error"] != nil {
		t.Fatalf("unexpected subscribe response: %+v", subscribe)
	}

	writeLine(t, conn, `{"id":2,"method":"mining.authorize","params":["worker","x"]}`)
	var authorize map[string]any
	readJSONLine(t, reader, &authorize)
	if authorize["result"] != true {
		t.Fatalf("unexpected authorize response: %+v", authorize)
	}
	var difficulty map[string]any
	readJSONLine(t, reader, &difficulty)
	if difficulty["method"] != "mining.set_difficulty" {
		t.Fatalf("unexpected difficulty message: %+v", difficulty)
	}
	var notify map[string]any
	readJSONLine(t, reader, &notify)
	if notify["method"] != "mining.notify" {
		t.Fatalf("unexpected notify message: %+v", notify)
	}
	params := notify["params"].([]any)
	jobID := params[0].(string)
	ntime := params[4].(string)
	nonce := solveNonce(t, template.HeaderHex, template.Bits, ntime)

	writeLine(t, conn, fmt.Sprintf(`{"id":3,"method":"mining.submit","params":["worker","%s","","%s","%s"]}`, jobID, ntime, nonce))
	var submit map[string]any
	readJSONLine(t, reader, &submit)
	if submit["result"] != true {
		t.Fatalf("unexpected submit response: %+v", submit)
	}
	var difficultyUpdate map[string]any
	readJSONLine(t, reader, &difficultyUpdate)
	if difficultyUpdate["method"] != "mining.set_difficulty" {
		t.Fatalf("unexpected post-submit difficulty message: %+v", difficultyUpdate)
	}
	if provider.connected != 1 || provider.activeJobs != 1 {
		t.Fatalf("unexpected stratum stats: connected=%d jobs=%d", provider.connected, provider.activeJobs)
	}
	if provider.accepted != 1 || provider.rejected != 0 || provider.solved != 1 || provider.lastWorker != "worker" {
		t.Fatalf("unexpected share accounting: %+v", provider)
	}
	if provider.blockHeight != 16 || provider.blockHash != "blockhash" {
		t.Fatalf("unexpected solved block attribution: %+v", provider)
	}
	if provider.blockHex == "" {
		t.Fatal("expected solved block to be submitted")
	}
	blockBytes, err := hex.DecodeString(provider.blockHex)
	if err != nil {
		t.Fatal(err)
	}
	headerBytes := blockBytes[:headerLength]
	if got := binary.LittleEndian.Uint32(headerBytes[headerHeightOffset:headerLength]); got != 16 {
		t.Fatalf("submitted block height = %d, want 16", got)
	}
}

func TestSubmitAcceptsShareWithoutBlockSolve(t *testing.T) {
	header := make([]byte, headerLength)
	binary.LittleEndian.PutUint64(header[headerTimestampOffset:headerBitsOffset], 1)
	binary.LittleEndian.PutUint32(header[headerBitsOffset:headerNonceOffset], 0x206bc47f)
	binary.LittleEndian.PutUint32(header[headerHeightOffset:headerLength], 17)
	block := append(append([]byte(nil), header...), 0x00)
	template := upstream.BlockTemplate{
		Height:            17,
		PreviousBlockHash: strings.Repeat("2", 64),
		Bits:              "206bc47f",
		Timestamp:         1,
		CoinbaseTxID:      strings.Repeat("3", 64),
		HeaderHex:         hex.EncodeToString(header),
		BlockHex:          hex.EncodeToString(block),
	}
	provider := &fakeSvc{template: template, shareDiff: 1}
	addr := freeAddr(t)
	server := New(addr, provider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = server.Run(ctx)
	}()
	waitForTCP(t, addr)
	server.updateJob(template)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)

	writeLine(t, conn, `{"id":1,"method":"mining.subscribe","params":[]}`)
	var subscribe map[string]any
	readJSONLine(t, reader, &subscribe)
	writeLine(t, conn, `{"id":2,"method":"mining.authorize","params":["worker.share","x"]}`)
	var authorize map[string]any
	readJSONLine(t, reader, &authorize)
	var difficulty map[string]any
	readJSONLine(t, reader, &difficulty)
	var notify map[string]any
	readJSONLine(t, reader, &notify)

	params := notify["params"].([]any)
	jobID := params[0].(string)
	ntime := params[4].(string)
	nonce := solveShareNonce(t, template.HeaderHex, template.Bits, ntime, provider.shareDiff)

	writeLine(t, conn, fmt.Sprintf(`{"id":3,"method":"mining.submit","params":["worker.share","%s","","%s","%s"]}`, jobID, ntime, nonce))
	var submit map[string]any
	readJSONLine(t, reader, &submit)
	if submit["result"] != true {
		t.Fatalf("unexpected submit response: %+v", submit)
	}
	var difficultyUpdate map[string]any
	readJSONLine(t, reader, &difficultyUpdate)
	if difficultyUpdate["method"] != "mining.set_difficulty" {
		t.Fatalf("unexpected post-submit difficulty message: %+v", difficultyUpdate)
	}
	if provider.blockHex != "" {
		t.Fatalf("unexpected solved block submission: %s", provider.blockHex)
	}
	if provider.accepted != 1 || provider.rejected != 0 || provider.solved != 0 || provider.lastWorker != "worker.share" {
		t.Fatalf("unexpected share accounting: %+v", provider)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func waitForTCP(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server did not start on %s", addr)
}

func writeLine(t *testing.T, conn net.Conn, line string) {
	t.Helper()
	if _, err := fmt.Fprintf(conn, "%s\n", line); err != nil {
		t.Fatal(err)
	}
}

func readJSONLine(t *testing.T, reader *bufio.Reader, dest any) {
	t.Helper()
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(line, dest); err != nil {
		t.Fatal(err)
	}
}

func solveNonce(t *testing.T, headerHex string, bitsHex string, ntime string) string {
	t.Helper()
	header, err := hex.DecodeString(headerHex)
	if err != nil {
		t.Fatal(err)
	}
	ntimeVal, err := strconv.ParseUint(ntime, 16, 32)
	if err != nil {
		t.Fatal(err)
	}
	bitsVal, err := strconv.ParseUint(bitsHex, 16, 32)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(header[headerTimestampOffset:headerBitsOffset], uint64(ntimeVal))
	binary.LittleEndian.PutUint32(header[headerBitsOffset:headerNonceOffset], uint32(bitsVal))
	target := compactToBig(uint32(bitsVal))
	for nonce := uint32(0); ; nonce++ {
		binary.LittleEndian.PutUint32(header[headerNonceOffset:headerHeightOffset], nonce)
		hash := blake256.Sum256(header)
		if hashToBig(hash[:]).Cmp(target) <= 0 {
			return fmt.Sprintf("%08x", nonce)
		}
	}
}

func solveShareNonce(t *testing.T, headerHex string, bitsHex string, ntime string, shareDiff float64) string {
	t.Helper()
	header, err := hex.DecodeString(headerHex)
	if err != nil {
		t.Fatal(err)
	}
	ntimeVal, err := strconv.ParseUint(ntime, 16, 32)
	if err != nil {
		t.Fatal(err)
	}
	bitsVal, err := strconv.ParseUint(bitsHex, 16, 32)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(header[headerTimestampOffset:headerBitsOffset], uint64(ntimeVal))
	binary.LittleEndian.PutUint32(header[headerBitsOffset:headerNonceOffset], uint32(bitsVal))
	shareTarget := difficultyToTarget(shareDiff)
	networkTarget := compactToBig(uint32(bitsVal))
	for nonce := uint32(0); ; nonce++ {
		binary.LittleEndian.PutUint32(header[headerNonceOffset:headerHeightOffset], nonce)
		hash := blake256.Sum256(header)
		value := hashToBig(hash[:])
		if value.Cmp(shareTarget) <= 0 && value.Cmp(networkTarget) > 0 {
			return fmt.Sprintf("%08x", nonce)
		}
	}
}
