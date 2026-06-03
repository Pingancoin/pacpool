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
	workers     []string
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

func (f *fakeSvc) SetStratumStats(connected int, jobs int, workers []string) {
	f.connected = connected
	f.activeJobs = jobs
	f.workers = append([]string(nil), workers...)
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
	withEasyDiffOneTarget(t)

	header := make([]byte, headerLength)
	putHeaderFields(header, 0x207fffff, 1, 16)
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

	writeLine(t, conn, `{"id":1,"method":"mining.subscribe","params":["cgminer/4.9.0","00000001"]}`)
	var subscribe map[string]any
	readJSONLine(t, reader, &subscribe)
	if subscribe["error"] != nil {
		t.Fatalf("unexpected subscribe response: %+v", subscribe)
	}
	subscribeResult := subscribe["result"].([]any)
	if got := len(subscribeResult[1].(string)); got != extraNonce1Size*2 {
		t.Fatalf("subscribe extranonce1 hex length = %d, want %d", got, extraNonce1Size*2)
	}
	if got := int(subscribeResult[2].(float64)); got != dr5ExtraNonce2Size {
		t.Fatalf("subscribe extranonce2 size = %d, want %d", got, dr5ExtraNonce2Size)
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
	ntime := params[7].(string)
	if got := len(params); got != 9 {
		t.Fatalf("notify param count = %d, want 9", got)
	}
	if got, want := params[1].(string), hex.EncodeToString(header[4:36]); got != want {
		t.Fatalf("notify prevblock = %q, want %q", got, want)
	}
	if got, want := params[2].(string), hex.EncodeToString(header[36:headerExtraDataOffset]); got != want {
		t.Fatalf("notify gen tx1 = %q, want %q", got, want)
	}
	if got, want := params[3].(string), hex.EncodeToString(header[headerExtraDataOffset+extraNonce1Size+dr5ExtraNonce2Size:headerLength]); got != want {
		t.Fatalf("notify gen tx2 = %q, want %q", got, want)
	}
	if branches, ok := params[4].([]any); !ok || len(branches) != 0 {
		t.Fatalf("notify branches = %#v, want empty array", params[4])
	}
	if got, want := params[5].(string), "07000000"; got != want {
		t.Fatalf("notify version = %q, want %q", got, want)
	}
	if got, want := params[6].(string), hex.EncodeToString(header[headerBitsOffset:headerBitsOffset+4]); got != want {
		t.Fatalf("notify bits = %q, want %q", got, want)
	}
	nonce := solveNonceWithVersion(t, template.HeaderHex, template.Bits, ntime, dr5HeaderVersion)

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
	if got := binary.LittleEndian.Uint32(headerBytes[headerHeightOffset : headerHeightOffset+4]); got != 16 {
		t.Fatalf("submitted block height = %d, want 16", got)
	}
}

func TestSubmitAcceptsShareWithoutBlockSolve(t *testing.T) {
	withEasyDiffOneTarget(t)

	header := make([]byte, headerLength)
	putHeaderFields(header, 0x206bc47f, 1, 17)
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

	writeLine(t, conn, `{"id":1,"method":"mining.subscribe","params":["cgminer/4.9.0","00000001"]}`)
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
	ntime := params[7].(string)
	if got, want := params[2].(string), hex.EncodeToString(header[36:headerExtraDataOffset]); got != want {
		t.Fatalf("notify partial header = %q, want %q", got, want)
	}
	nonce := solveShareNonceWithVersion(t, template.HeaderHex, template.Bits, ntime, provider.shareDiff, dr5HeaderVersion)

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

func TestCommonMinerHandshakeMethods(t *testing.T) {
	template := upstream.BlockTemplate{
		Height:            18,
		PreviousBlockHash: strings.Repeat("4", 64),
		Bits:              "207fffff",
		Timestamp:         1,
		CoinbaseTxID:      strings.Repeat("5", 64),
		HeaderHex:         strings.Repeat("00", headerLength),
		BlockHex:          strings.Repeat("00", headerLength+1),
	}
	provider := &fakeSvc{template: template, shareDiff: 2}
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

	writeLine(t, conn, `{"id":1,"method":"mining.configure","params":[["minimum-difficulty","version-rolling"],{}]}`)
	var configure map[string]any
	readJSONLine(t, reader, &configure)
	if configure["error"] != nil {
		t.Fatalf("configure failed: %+v", configure)
	}
	result := configure["result"].(map[string]any)
	if result["minimum-difficulty"] != true || result["version-rolling"] != false {
		t.Fatalf("unexpected configure result: %+v", configure)
	}

	writeLine(t, conn, `{"id":2,"method":"mining.extranonce.subscribe","params":[]}`)
	var extranonce map[string]any
	readJSONLine(t, reader, &extranonce)
	if extranonce["result"] != true {
		t.Fatalf("unexpected extranonce response: %+v", extranonce)
	}

	writeLine(t, conn, `{"id":3,"method":"mining.suggest_difficulty","params":[4]}`)
	var suggest map[string]any
	readJSONLine(t, reader, &suggest)
	if suggest["result"] != true {
		t.Fatalf("unexpected suggest response: %+v", suggest)
	}
	var difficulty map[string]any
	readJSONLine(t, reader, &difficulty)
	if difficulty["method"] != "mining.set_difficulty" {
		t.Fatalf("expected difficulty after suggest: %+v", difficulty)
	}
	params := difficulty["params"].([]any)
	if params[0].(float64) != 4 {
		t.Fatalf("suggested difficulty not applied: %+v", difficulty)
	}

	writeLine(t, conn, `{"id":4,"method":"client.get_version","params":[]}`)
	var version map[string]any
	readJSONLine(t, reader, &version)
	if !strings.Contains(version["result"].(string), "pacpool") {
		t.Fatalf("unexpected version response: %+v", version)
	}

	writeLine(t, conn, `{"id":5,"method":"mining.get_transactions","params":["job"]}`)
	var txs map[string]any
	readJSONLine(t, reader, &txs)
	if txs["error"] != nil {
		t.Fatalf("unexpected transactions response: %+v", txs)
	}
}

func TestLegacyPacminerNotifyUsesHeaderHexExtension(t *testing.T) {
	header := make([]byte, headerLength)
	putHeaderFields(header, 0x207fffff, 42, 21)
	template := upstream.BlockTemplate{
		Height:            21,
		PreviousBlockHash: strings.Repeat("a", 64),
		Bits:              "207fffff",
		Timestamp:         42,
		CoinbaseTxID:      strings.Repeat("b", 64),
		HeaderHex:         hex.EncodeToString(header),
		BlockHex:          hex.EncodeToString(append(append([]byte(nil), header...), 0x00)),
	}
	provider := &fakeSvc{template: template, shareDiff: 1024}
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

	writeLine(t, conn, `{"id":1,"method":"mining.subscribe","params":["pacminer/0.3.0"]}`)
	var subscribe map[string]any
	readJSONLine(t, reader, &subscribe)
	result := subscribe["result"].([]any)
	if got := int(result[2].(float64)); got != 0 {
		t.Fatalf("legacy extranonce2 size = %d, want 0", got)
	}

	writeLine(t, conn, `{"id":2,"method":"mining.authorize","params":["worker.gpu","x"]}`)
	var authorize map[string]any
	readJSONLine(t, reader, &authorize)
	var difficulty map[string]any
	readJSONLine(t, reader, &difficulty)
	var notify map[string]any
	readJSONLine(t, reader, &notify)
	if notify["method"] != "mining.notify" {
		t.Fatalf("unexpected notify message: %+v", notify)
	}
	params := notify["params"].([]any)
	if got := len(params); got != 7 {
		t.Fatalf("legacy notify param count = %d, want 7", got)
	}
	if got := params[6].(string); got != template.HeaderHex {
		t.Fatalf("legacy headerhex = %q, want %q", got, template.HeaderHex)
	}
}

func TestSuggestedDifficultySurvivesAuthorize(t *testing.T) {
	template := upstream.BlockTemplate{
		Height:            19,
		PreviousBlockHash: strings.Repeat("6", 64),
		Bits:              "207fffff",
		Timestamp:         1,
		CoinbaseTxID:      strings.Repeat("7", 64),
		HeaderHex:         strings.Repeat("00", headerLength),
		BlockHex:          strings.Repeat("00", headerLength+1),
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

	writeLine(t, conn, `{"id":1,"method":"mining.subscribe","params":["cgminer/4.9.0","00000001"]}`)
	var subscribe map[string]any
	readJSONLine(t, reader, &subscribe)

	writeLine(t, conn, `{"id":2,"method":"mining.suggest_difficulty","params":[4]}`)
	var suggest map[string]any
	readJSONLine(t, reader, &suggest)
	var suggestedDiff map[string]any
	readJSONLine(t, reader, &suggestedDiff)
	if suggestedDiff["params"].([]any)[0].(float64) != 4 {
		t.Fatalf("suggested difficulty not sent: %+v", suggestedDiff)
	}

	writeLine(t, conn, `{"id":3,"method":"mining.authorize","params":["worker.compat","x"]}`)
	var authorize map[string]any
	readJSONLine(t, reader, &authorize)
	if authorize["result"] != true {
		t.Fatalf("unexpected authorize response: %+v", authorize)
	}
	var authorizedDiff map[string]any
	readJSONLine(t, reader, &authorizedDiff)
	if authorizedDiff["params"].([]any)[0].(float64) != 4 {
		t.Fatalf("authorize overwrote suggested difficulty: %+v", authorizedDiff)
	}
}

func TestSuggestedDifficultySurvivesAcceptedShare(t *testing.T) {
	withEasyDiffOneTarget(t)

	header := make([]byte, headerLength)
	putHeaderFields(header, 0x206bc47f, 1, 20)
	block := append(append([]byte(nil), header...), 0x00)
	template := upstream.BlockTemplate{
		Height:            20,
		PreviousBlockHash: strings.Repeat("8", 64),
		Bits:              "206bc47f",
		Timestamp:         1,
		CoinbaseTxID:      strings.Repeat("9", 64),
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

	writeLine(t, conn, `{"id":1,"method":"mining.subscribe","params":["cgminer/4.9.0","00000001"]}`)
	var subscribe map[string]any
	readJSONLine(t, reader, &subscribe)

	writeLine(t, conn, `{"id":2,"method":"mining.suggest_difficulty","params":[1]}`)
	var suggest map[string]any
	readJSONLine(t, reader, &suggest)
	var suggestedDiff map[string]any
	readJSONLine(t, reader, &suggestedDiff)
	if suggestedDiff["params"].([]any)[0].(float64) != 1 {
		t.Fatalf("suggested difficulty not sent: %+v", suggestedDiff)
	}

	writeLine(t, conn, `{"id":3,"method":"mining.authorize","params":["worker.fixed","x"]}`)
	var authorize map[string]any
	readJSONLine(t, reader, &authorize)
	var authorizedDiff map[string]any
	readJSONLine(t, reader, &authorizedDiff)
	var notify map[string]any
	readJSONLine(t, reader, &notify)
	params := notify["params"].([]any)
	jobID := params[0].(string)
	ntime := params[7].(string)
	nonce := solveShareNonceWithVersion(t, template.HeaderHex, template.Bits, ntime, 1, dr5HeaderVersion)

	writeLine(t, conn, fmt.Sprintf(`{"id":4,"method":"mining.submit","params":["worker.fixed","%s","","%s","%s"]}`, jobID, ntime, nonce))
	var submit map[string]any
	readJSONLine(t, reader, &submit)
	if submit["result"] != true {
		t.Fatalf("unexpected submit response: %+v", submit)
	}

	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	line, err := reader.ReadBytes('\n')
	_ = conn.SetReadDeadline(time.Time{})
	if err == nil {
		var extra map[string]any
		if json.Unmarshal(line, &extra) == nil && extra["method"] == "mining.set_difficulty" {
			t.Fatalf("fixed suggested difficulty was overwritten after share: %+v", extra)
		}
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

func putHeaderFields(header []byte, bits uint32, timestamp uint32, height uint32) {
	binary.LittleEndian.PutUint32(header[headerTimestampOffset:headerTimestampOffset+4], timestamp)
	binary.LittleEndian.PutUint32(header[headerBitsOffset:headerBitsOffset+4], bits)
	binary.LittleEndian.PutUint32(header[headerHeightOffset:headerHeightOffset+4], height)
}

func withEasyDiffOneTarget(t *testing.T) {
	t.Helper()
	original := dcrDiffOneTarget
	dcrDiffOneTarget = compactToBig(0x207fffff)
	t.Cleanup(func() {
		dcrDiffOneTarget = original
	})
}

func solveNonce(t *testing.T, headerHex string, bitsHex string, ntime string) string {
	return solveNonceWithVersion(t, headerHex, bitsHex, ntime, 0)
}

func solveNonceWithVersion(t *testing.T, headerHex string, bitsHex string, ntime string, version uint32) string {
	t.Helper()
	header, err := hex.DecodeString(headerHex)
	if err != nil {
		t.Fatal(err)
	}
	if version > 0 {
		binary.LittleEndian.PutUint32(header[headerVersionOffset:headerVersionOffset+4], version)
	}
	ntimeBytes, err := hex.DecodeString(ntime)
	if err != nil {
		t.Fatal(err)
	}
	if len(ntimeBytes) != 4 {
		t.Fatalf("ntime length = %d, want 4", len(ntimeBytes))
	}
	bitsVal, err := strconv.ParseUint(bitsHex, 16, 32)
	if err != nil {
		t.Fatal(err)
	}
	copy(header[headerTimestampOffset:headerTimestampOffset+4], ntimeBytes)
	binary.LittleEndian.PutUint32(header[headerBitsOffset:headerBitsOffset+4], uint32(bitsVal))
	target := compactToBig(uint32(bitsVal))
	for nonce := uint32(0); ; nonce++ {
		binary.LittleEndian.PutUint32(header[headerNonceOffset:headerNonceOffset+4], nonce)
		hash := blake256.Sum256(header)
		if hashToBig(hash[:]).Cmp(target) <= 0 {
			return hex.EncodeToString(header[headerNonceOffset : headerNonceOffset+4])
		}
	}
}

func solveShareNonce(t *testing.T, headerHex string, bitsHex string, ntime string, shareDiff float64) string {
	return solveShareNonceWithVersion(t, headerHex, bitsHex, ntime, shareDiff, 0)
}

func solveShareNonceWithVersion(t *testing.T, headerHex string, bitsHex string, ntime string, shareDiff float64, version uint32) string {
	t.Helper()
	header, err := hex.DecodeString(headerHex)
	if err != nil {
		t.Fatal(err)
	}
	if version > 0 {
		binary.LittleEndian.PutUint32(header[headerVersionOffset:headerVersionOffset+4], version)
	}
	ntimeBytes, err := hex.DecodeString(ntime)
	if err != nil {
		t.Fatal(err)
	}
	if len(ntimeBytes) != 4 {
		t.Fatalf("ntime length = %d, want 4", len(ntimeBytes))
	}
	bitsVal, err := strconv.ParseUint(bitsHex, 16, 32)
	if err != nil {
		t.Fatal(err)
	}
	copy(header[headerTimestampOffset:headerTimestampOffset+4], ntimeBytes)
	binary.LittleEndian.PutUint32(header[headerBitsOffset:headerBitsOffset+4], uint32(bitsVal))
	shareTarget := difficultyToTarget(shareDiff, dcrDiffOneTarget)
	networkTarget := compactToBig(uint32(bitsVal))
	for nonce := uint32(0); ; nonce++ {
		binary.LittleEndian.PutUint32(header[headerNonceOffset:headerNonceOffset+4], nonce)
		hash := blake256.Sum256(header)
		value := hashToBig(hash[:])
		if value.Cmp(shareTarget) <= 0 && value.Cmp(networkTarget) > 0 {
			return hex.EncodeToString(header[headerNonceOffset : headerNonceOffset+4])
		}
	}
}
