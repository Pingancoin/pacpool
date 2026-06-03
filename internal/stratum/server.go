package stratum

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Pingancoin/pacpool/internal/upstream"
	"github.com/decred/dcrd/crypto/blake256"
)

type TemplateProvider interface {
	CurrentTemplate() (upstream.BlockTemplate, bool)
	SubmitSolvedBlock(context.Context, string) (bool, uint32, string, error)
	ShareDifficulty() float64
	WorkerDifficulty(worker string) float64
	SetStratumStats(connected int, jobs int, workerNames []string)
	RecordShare(worker string, accepted bool, solved bool, reason string)
	RecordSolvedBlock(worker string, height uint32, hash string)
}

type Server struct {
	listen string
	svc    TemplateProvider
	debug  bool

	mu       sync.RWMutex
	job      *Job
	sessions map[*session]struct{}
	nextID   atomic.Uint64
}

type Job struct {
	ID         string
	Template   upstream.BlockTemplate
	HeaderHex  string
	BlockHex   string
	TargetBits uint32
}

const (
	extraNonce1Size        = 4
	defaultExtraNonce2Size = 8
	dr5ExtraNonce2Size     = 8
	dr5HeaderVersion       = 7
	minJobRefresh          = 30 * time.Second

	headerVersionOffset   = 0
	headerBitsOffset      = 116
	headerHeightOffset    = 128
	headerTimestampOffset = 136
	headerNonceOffset     = 140
	headerExtraDataOffset = 144
	headerLength          = 180
)

var (
	dcrDiffOneTarget    = compactToBig(0x1d00ffff)
	legacyDiffOneTarget = compactToBig(0x207fffff)
)

type request struct {
	ID     any               `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

type response struct {
	ID     any    `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  any    `json:"error"`
	Method string `json:"method,omitempty"`
	Params any    `json:"params,omitempty"`
}

type notification struct {
	ID     any    `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type session struct {
	conn        net.Conn
	server      *Server
	writerMu    sync.Mutex
	sessionID   string
	extraNonce1 string
	subscribed  bool
	authorized  bool
	worker      string
	difficulty  float64
	diffSent    bool
	fixedDiff   bool
	legacy      bool
	dr5         bool
}

func New(listen string, svc TemplateProvider) *Server {
	return NewWithOptions(listen, svc, Options{})
}

type Options struct {
	Debug bool
}

func NewWithOptions(listen string, svc TemplateProvider, opts Options) *Server {
	return &Server{
		listen:   listen,
		svc:      svc,
		debug:    opts.Debug,
		sessions: make(map[*session]struct{}),
	}
}

func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.listen)
	if err != nil {
		return err
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	go s.templateLoop(ctx)

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		sess := &session{
			conn:        conn,
			server:      s,
			sessionID:   fmt.Sprintf("%08x", s.nextID.Add(1)),
			extraNonce1: randomHex(extraNonce1Size),
		}
		s.addSession(sess)
		go sess.run(ctx)
	}
}

func (s *Server) templateLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			template, ok := s.svc.CurrentTemplate()
			if !ok {
				continue
			}
			s.updateJob(template)
		}
	}
}

func (s *Server) updateJob(template upstream.BlockTemplate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job != nil && sameTemplate(s.job.Template, template) {
		s.publishStatsLocked()
		return
	}
	clean := true
	if s.job != nil && sameWorkIdentity(s.job.Template, template) {
		clean = false
	}
	bits, _ := strconv.ParseUint(template.Bits, 16, 32)
	job := &Job{
		ID:         stratumJobID(template.Height),
		Template:   template,
		HeaderHex:  template.HeaderHex,
		BlockHex:   template.BlockHex,
		TargetBits: uint32(bits),
	}
	s.job = job
	s.publishStatsLocked()
	for sess := range s.sessions {
		if sess.authorized && sess.subscribed {
			shareDiff := s.svc.ShareDifficulty()
			if sess.difficulty > 0 {
				shareDiff = sess.difficulty
			} else if sess.worker != "" {
				shareDiff = s.svc.WorkerDifficulty(sess.worker)
			}
			if !sess.diffSent || sess.difficulty <= 0 || !nearlyEqual(sess.difficulty, shareDiff) {
				sess.difficulty = shareDiff
				_ = sess.sendDifficulty(shareDiff)
			}
			_ = sess.sendNotify(job, clean)
		}
	}
}

func sameTemplate(a upstream.BlockTemplate, b upstream.BlockTemplate) bool {
	if !sameWorkIdentity(a, b) {
		return false
	}
	return b.Timestamp-a.Timestamp < int64(minJobRefresh/time.Second)
}

func sameWorkIdentity(a upstream.BlockTemplate, b upstream.BlockTemplate) bool {
	if a.Height != b.Height ||
		a.PreviousBlockHash != b.PreviousBlockHash ||
		a.CoinbaseTxID != b.CoinbaseTxID ||
		a.Bits != b.Bits ||
		!sameStrings(a.TransactionIDs, b.TransactionIDs) {
		return false
	}
	return true
}

func sameStrings(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *Server) addSession(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess] = struct{}{}
	s.publishStatsLocked()
}

func (s *Server) removeSession(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sess)
	s.publishStatsLocked()
}

func (s *Server) publishStatsLocked() {
	jobCount := 0
	if s.job != nil {
		jobCount = 1
	}
	workers := make([]string, 0, len(s.sessions))
	for sess := range s.sessions {
		if sess.authorized && sess.worker != "" {
			workers = append(workers, sess.worker)
		}
	}
	s.svc.SetStratumStats(len(s.sessions), jobCount, workers)
}

func (s *Server) currentJob() *Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.job == nil {
		return nil
	}
	job := *s.job
	return &job
}

func (sess *session) run(ctx context.Context) {
	defer sess.conn.Close()
	defer sess.server.removeSession(sess)

	reader := bufio.NewScanner(sess.conn)
	reader.Buffer(make([]byte, 0, 4096), 1024*1024)
	for reader.Scan() {
		sess.logWire("recv", reader.Text())
		var req request
		if err := json.Unmarshal(reader.Bytes(), &req); err != nil {
			_ = sess.sendResponse(response{ID: nil, Error: []any{20, "invalid json", nil}})
			continue
		}
		if err := sess.handle(ctx, req); err != nil {
			_ = sess.sendResponse(response{ID: req.ID, Error: []any{20, err.Error(), nil}})
		}
	}
	if err := reader.Err(); err != nil {
		log.Printf("pacpool stratum read %s: %v", sess.conn.RemoteAddr(), err)
	}
}

func (sess *session) handle(ctx context.Context, req request) error {
	switch req.Method {
	case "mining.configure":
		return sess.sendResponse(response{
			ID: req.ID,
			Result: map[string]bool{
				"minimum-difficulty":   true,
				"subscribe-extranonce": false,
				"version-rolling":      false,
			},
			Error: nil,
		})
	case "mining.subscribe":
		sess.legacy = true
		var userAgent string
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params[0], &userAgent)
		}
		userAgentLower := strings.ToLower(userAgent)
		if strings.Contains(userAgentLower, "cgminer/4.9.0") {
			sess.legacy = false
			sess.dr5 = true
		}
		sess.subscribed = true
		if sess.legacy {
			return sess.sendResponse(response{
				ID: req.ID,
				Result: []any{
					[][]string{{"mining.notify", sess.sessionID}, {"mining.set_difficulty", sess.sessionID}},
					sess.sessionID,
					0,
				},
				Error: nil,
			})
		}
		return sess.sendResponse(response{
			ID: req.ID,
			Result: []any{
				[][]string{{"mining.set_difficulty", sess.sessionID}, {"mining.notify", sess.sessionID}},
				sess.subscribeExtraNonce1(),
				sess.extraNonce2Size(),
			},
			Error: nil,
		})
	case "mining.authorize":
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params[0], &sess.worker)
		}
		sess.authorized = true
		if !sess.fixedDiff {
			workerDiff := sess.server.svc.WorkerDifficulty(sess.worker)
			if workerDiff > sess.difficulty {
				sess.difficulty = workerDiff
			}
		}
		sess.server.mu.Lock()
		sess.server.publishStatsLocked()
		sess.server.mu.Unlock()
		if err := sess.sendResponse(response{ID: req.ID, Result: true, Error: nil}); err != nil {
			return err
		}
		if err := sess.sendDifficulty(sess.currentDifficulty()); err != nil {
			return err
		}
		if job := sess.server.currentJob(); job != nil && sess.subscribed {
			time.Sleep(time.Second)
			return sess.sendNotify(job, true)
		}
		return nil
	case "mining.extranonce.subscribe":
		return sess.sendResponse(response{ID: req.ID, Result: true, Error: nil})
	case "mining.suggest_difficulty":
		if len(req.Params) > 0 {
			var suggested float64
			if err := json.Unmarshal(req.Params[0], &suggested); err == nil && suggested > 0 {
				base := sess.server.svc.ShareDifficulty()
				if suggested < base {
					suggested = base
				}
				sess.difficulty = suggested
				sess.fixedDiff = true
			}
		}
		if err := sess.sendResponse(response{ID: req.ID, Result: true, Error: nil}); err != nil {
			return err
		}
		return sess.sendDifficulty(sess.currentDifficulty())
	case "mining.suggest_target":
		return sess.sendResponse(response{ID: req.ID, Result: true, Error: nil})
	case "client.get_version":
		return sess.sendResponse(response{ID: req.ID, Result: "pacpool/0.1.0", Error: nil})
	case "mining.get_transactions":
		return sess.sendResponse(response{ID: req.ID, Result: []string{}, Error: nil})
	case "mining.submit":
		return sess.handleSubmit(ctx, req)
	default:
		return sess.sendResponse(response{ID: req.ID, Result: nil, Error: []any{20, "unsupported method", nil}})
	}
}

func (sess *session) handleSubmit(ctx context.Context, req request) error {
	if !sess.authorized || !sess.subscribed {
		sess.server.svc.RecordShare(sess.worker, false, false, "not subscribed or authorized")
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: []any{24, "not subscribed or authorized", nil}})
	}
	if len(req.Params) < 5 {
		sess.server.svc.RecordShare(sess.worker, false, false, "invalid submit params")
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: []any{25, "invalid submit params", nil}})
	}
	var worker, jobID, extranonce2, ntimeHex, nonceHex string
	_ = json.Unmarshal(req.Params[0], &worker)
	_ = json.Unmarshal(req.Params[1], &jobID)
	_ = json.Unmarshal(req.Params[2], &extranonce2)
	_ = json.Unmarshal(req.Params[3], &ntimeHex)
	_ = json.Unmarshal(req.Params[4], &nonceHex)
	if worker == "" {
		worker = sess.worker
	}

	job := sess.server.currentJob()
	if job == nil || job.ID != jobID {
		sess.server.svc.RecordShare(worker, false, false, "stale job")
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: []any{21, "stale job", nil}})
	}
	headerBytes, err := hex.DecodeString(job.HeaderHex)
	if err != nil {
		sess.server.svc.RecordShare(worker, false, false, "bad template header")
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: []any{20, "bad template header", nil}})
	}
	blockBytes, err := hex.DecodeString(job.BlockHex)
	if err != nil {
		sess.server.svc.RecordShare(worker, false, false, "bad template block")
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: []any{20, "bad template block", nil}})
	}
	if len(headerBytes) < headerLength || len(blockBytes) < headerLength {
		sess.server.svc.RecordShare(worker, false, false, "short template")
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: []any{20, "short template", nil}})
	}
	if sess.dr5 {
		binary.LittleEndian.PutUint32(headerBytes[headerVersionOffset:headerVersionOffset+4], dr5HeaderVersion)
	}
	reverseSubmitWords := true
	ntimeBytes, err := decodeUint32Hex(ntimeHex, reverseSubmitWords)
	if err != nil {
		sess.server.svc.RecordShare(worker, false, false, "invalid ntime")
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: []any{22, "invalid ntime", nil}})
	}
	ntime := binary.LittleEndian.Uint32(ntimeBytes)
	nonceBytes, err := decodeUint32Hex(nonceHex, reverseSubmitWords)
	if err != nil {
		sess.server.svc.RecordShare(worker, false, false, "invalid nonce")
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: []any{22, "invalid nonce", nil}})
	}
	if int64(ntime) < job.Template.Timestamp {
		sess.server.svc.RecordShare(worker, false, false, "ntime before template")
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: []any{23, "ntime before template", nil}})
	}
	extraData, err := submitExtraData(sess.extraNonce1, extranonce2)
	if err != nil {
		sess.server.svc.RecordShare(worker, false, false, err.Error())
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: []any{22, err.Error(), nil}})
	}

	copy(headerBytes[headerTimestampOffset:headerTimestampOffset+4], ntimeBytes)
	binary.LittleEndian.PutUint32(headerBytes[headerBitsOffset:headerBitsOffset+4], job.TargetBits)
	copy(headerBytes[headerNonceOffset:headerNonceOffset+4], nonceBytes)
	copy(headerBytes[headerExtraDataOffset:headerExtraDataOffset+len(extraData)], extraData[:])
	copy(blockBytes[:headerLength], headerBytes[:headerLength])

	hash := blake256.Sum256(headerBytes[:headerLength])
	networkTarget := compactToBig(job.TargetBits)
	shareTarget := sess.difficultyTarget()
	hashValue := hashToBig(hash[:])
	if hashValue.Cmp(shareTarget) > 0 {
		sess.server.svc.RecordShare(worker, false, false, "low difficulty share")
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: []any{23, "low difficulty share", nil}})
	}
	if hashValue.Cmp(networkTarget) > 0 {
		sess.server.svc.RecordShare(worker, true, false, "")
		return sess.sendAccepted(req.ID, worker, false)
	}
	accepted, height, blockHash, err := sess.server.svc.SubmitSolvedBlock(ctx, hex.EncodeToString(blockBytes))
	if err != nil {
		sess.server.svc.RecordShare(worker, false, false, err.Error())
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: []any{20, err.Error(), nil}})
	}
	sess.server.svc.RecordShare(worker, accepted, accepted, "")
	_ = height
	_ = blockHash
	if !accepted {
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: nil})
	}
	if blockHash != "" || height > 0 {
		sess.server.svc.RecordSolvedBlock(worker, height, blockHash)
	}
	return sess.sendAccepted(req.ID, worker, true)
}

func (sess *session) sendDifficulty(difficulty float64) error {
	if difficulty <= 0 {
		difficulty = sess.server.svc.ShareDifficulty()
	}
	if err := sess.sendNotification("mining.set_difficulty", []any{difficulty}); err != nil {
		return err
	}
	sess.difficulty = difficulty
	sess.diffSent = true
	return nil
}

func (sess *session) sendNotification(method string, params any) error {
	return sess.sendJSON(notification{
		ID:     nil,
		Method: method,
		Params: params,
	})
}

func (sess *session) currentDifficulty() float64 {
	if sess.difficulty > 0 {
		return sess.difficulty
	}
	return sess.server.svc.ShareDifficulty()
}

func (sess *session) difficultyTarget() *big.Int {
	base := dcrDiffOneTarget
	if sess.legacy {
		base = legacyDiffOneTarget
	}
	return difficultyToTarget(sess.currentDifficulty(), base)
}

func (sess *session) sendNotify(job *Job, clean bool) error {
	headerBytes, err := hex.DecodeString(job.HeaderHex)
	if err != nil {
		return err
	}
	if len(headerBytes) < headerLength {
		return fmt.Errorf("short template header")
	}
	if sess.legacy {
		return sess.sendNotification("mining.notify", []any{
			job.ID,
			job.Template.PreviousBlockHash,
			job.Template.CoinbaseTxID,
			job.Template.Bits,
			fmt.Sprintf("%08x", uint32(job.Template.Timestamp)),
			clean,
			job.HeaderHex,
		})
	}
	if sess.dr5 {
		binary.LittleEndian.PutUint32(headerBytes[headerVersionOffset:headerVersionOffset+4], dr5HeaderVersion)
		prevBlock, err := reversePrevBlockWords(hex.EncodeToString(headerBytes[4:36]))
		if err != nil {
			return err
		}
		bits, err := reverseHexBytes(hex.EncodeToString(headerBytes[headerBitsOffset : headerBitsOffset+4]))
		if err != nil {
			return err
		}
		ntime, err := reverseHexBytes(hex.EncodeToString(headerBytes[headerTimestampOffset : headerTimestampOffset+4]))
		if err != nil {
			return err
		}
		// Match the pre-v2 dcrpool DR3/DR5 wire format. Antminer DR5 firmware
		// expects the Decred-style 9-parameter notify and returns the padded
		// 12-byte extranonce blob in mining.submit.
		return sess.sendNotification("mining.notify", []any{
			job.ID,
			prevBlock,
			hex.EncodeToString(headerBytes[36:headerExtraDataOffset]),
			hex.EncodeToString(headerBytes[176:headerLength]),
			[]string{},
			hex.EncodeToString(headerBytes[0:4]),
			bits,
			ntime,
			clean,
		})
	}
	prevBlock, err := reversePrevBlockWords(hex.EncodeToString(headerBytes[4:36]))
	if err != nil {
		return err
	}
	bits, err := reverseHexBytes(hex.EncodeToString(headerBytes[headerBitsOffset : headerBitsOffset+4]))
	if err != nil {
		return err
	}
	ntime, err := reverseHexBytes(hex.EncodeToString(headerBytes[headerTimestampOffset : headerTimestampOffset+4]))
	if err != nil {
		return err
	}
	return sess.sendNotification("mining.notify", []any{
		job.ID,
		prevBlock,
		hex.EncodeToString(headerBytes[36:144]),
		hex.EncodeToString(headerBytes[176:180]),
		[]string{},
		hex.EncodeToString(headerBytes[0:4]),
		bits,
		ntime,
		clean,
	})
}

func (sess *session) sendResponse(resp response) error {
	return sess.sendJSON(resp)
}

func (sess *session) sendJSON(v any) error {
	sess.writerMu.Lock()
	defer sess.writerMu.Unlock()
	encoded, err := json.Marshal(v)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	sess.logWire("send", strings.TrimRight(string(encoded), "\n"))
	_, err = sess.conn.Write(encoded)
	return err
}

func (sess *session) extraNonce2Size() int {
	if sess.dr5 {
		return dr5ExtraNonce2Size
	}
	return defaultExtraNonce2Size
}

func (sess *session) subscribeExtraNonce1() string {
	if sess.dr5 {
		return strings.Repeat("0", dr5ExtraNonce2Size*2) + sess.extraNonce1
	}
	return strings.Repeat("0", defaultExtraNonce2Size*2) + sess.extraNonce1
}

func (sess *session) logWire(direction string, line string) {
	if !sess.server.debug {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if len(line) > 2000 {
		line = line[:2000] + "...<truncated>"
	}
	log.Printf("pacpool stratum %s %s worker=%q dr5=%t legacy=%t %s", direction, sess.conn.RemoteAddr(), sess.worker, sess.dr5, sess.legacy, line)
}

func (sess *session) sendAccepted(id any, worker string, solved bool) error {
	if err := sess.sendResponse(response{ID: id, Result: true, Error: nil}); err != nil {
		return err
	}
	if sess.fixedDiff {
		return nil
	}
	nextDiff := sess.server.svc.WorkerDifficulty(worker)
	if nextDiff <= 0 || nearlyEqual(nextDiff, sess.difficulty) {
		return nil
	}
	sess.difficulty = nextDiff
	return sess.sendDifficulty(nextDiff)
}

func compactToBig(compact uint32) *big.Int {
	mantissa := compact & 0x007fffff
	isNegative := compact&0x00800000 != 0
	exponent := uint(compact >> 24)

	var bn *big.Int
	if exponent <= 3 {
		mantissa >>= 8 * (3 - exponent)
		bn = big.NewInt(int64(mantissa))
	} else {
		bn = big.NewInt(int64(mantissa))
		bn.Lsh(bn, 8*(exponent-3))
	}
	if isNegative {
		bn.Neg(bn)
	}
	return bn
}

func hashToBig(hash []byte) *big.Int {
	return new(big.Int).SetBytes(hash)
}

func submitExtraData(extraNonce1 string, submitted string) ([32]byte, error) {
	var extraData [32]byte
	submitted = strings.TrimSpace(submitted)
	if submitted == "" {
		return extraData, nil
	}
	submittedBytes, err := hex.DecodeString(submitted)
	if err != nil {
		return extraData, fmt.Errorf("invalid extranonce")
	}
	if len(submittedBytes) == defaultExtraNonce2Size && extraNonce1 != "" {
		suffix, err := hex.DecodeString(extraNonce1)
		if err != nil {
			return extraData, fmt.Errorf("invalid session extranonce")
		}
		submittedBytes = append(submittedBytes, suffix...)
	} else if len(submittedBytes) <= extraNonce1Size && extraNonce1 != "" {
		prefix, err := hex.DecodeString(extraNonce1)
		if err != nil {
			return extraData, fmt.Errorf("invalid session extranonce")
		}
		submittedBytes = append(prefix, submittedBytes...)
	}
	if len(submittedBytes) == 0 || len(submittedBytes) > len(extraData) {
		return extraData, fmt.Errorf("invalid extranonce length")
	}
	copy(extraData[:], submittedBytes)
	return extraData, nil
}

func decodeUint32Hex(value string, reverse bool) ([]byte, error) {
	decoded, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, err
	}
	if len(decoded) != 4 {
		return nil, fmt.Errorf("uint32 hex length is %d, want 4", len(decoded))
	}
	if reverse {
		for i, j := 0, len(decoded)-1; i < j; i, j = i+1, j-1 {
			decoded[i], decoded[j] = decoded[j], decoded[i]
		}
	}
	return decoded, nil
}

func reverseHexBytes(value string) (string, error) {
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return "", err
	}
	for i, j := 0, len(decoded)-1; i < j; i, j = i+1, j-1 {
		decoded[i], decoded[j] = decoded[j], decoded[i]
	}
	return hex.EncodeToString(decoded), nil
}

func reversePrevBlockWords(value string) (string, error) {
	if len(value)%8 != 0 {
		return "", fmt.Errorf("prevhash length must be a multiple of 4 bytes")
	}
	var b strings.Builder
	b.Grow(len(value))
	for i := 0; i < len(value); i += 8 {
		word, err := reverseHexBytes(value[i : i+8])
		if err != nil {
			return "", err
		}
		b.WriteString(word)
	}
	return b.String(), nil
}

func randomHex(size int) string {
	if size <= 0 {
		return ""
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		for i := range buf {
			buf[i] = byte(time.Now().UnixNano() >> (uint(i%8) * 8))
		}
	}
	return hex.EncodeToString(buf)
}

func stratumJobID(height uint32) string {
	var id [12]byte
	binary.BigEndian.PutUint32(id[:4], height)
	binary.BigEndian.PutUint64(id[4:], uint64(time.Now().UnixNano()))
	return hex.EncodeToString(id[:])
}

func difficultyToTarget(difficulty float64, baseTarget *big.Int) *big.Int {
	if difficulty <= 0 {
		difficulty = 1
	}
	base := new(big.Int).Set(baseTarget)
	scaled := new(big.Rat).SetInt(base)
	scaled.Quo(scaled, new(big.Rat).SetFloat64(difficulty))
	target := new(big.Int)
	scaled.Num().Quo(scaled.Num(), scaled.Denom())
	target.Set(scaled.Num())
	if target.Sign() <= 0 {
		return big.NewInt(1)
	}
	if target.Cmp(base) > 0 {
		return base
	}
	return target
}

func nearlyEqual(a float64, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.000001
}
