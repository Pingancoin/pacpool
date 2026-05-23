package stratum

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	headerTimestampOffset = 68
	headerBitsOffset      = 76
	headerNonceOffset     = 80
	headerHeightOffset    = 84
	headerLength          = 88
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

type session struct {
	conn       net.Conn
	server     *Server
	writerMu   sync.Mutex
	sessionID  string
	subscribed bool
	authorized bool
	worker     string
	difficulty float64
	fixedDiff  bool
}

func New(listen string, svc TemplateProvider) *Server {
	return &Server{
		listen:   listen,
		svc:      svc,
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
			conn:      conn,
			server:    s,
			sessionID: fmt.Sprintf("%08x", s.nextID.Add(1)),
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
	bits, _ := strconv.ParseUint(template.Bits, 16, 32)
	job := &Job{
		ID:         fmt.Sprintf("%08x", s.nextID.Add(1)),
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
			_ = sess.sendDifficulty(shareDiff)
			_ = sess.sendNotify(job, true)
		}
	}
}

func sameTemplate(a upstream.BlockTemplate, b upstream.BlockTemplate) bool {
	return a.Height == b.Height &&
		a.PreviousBlockHash == b.PreviousBlockHash &&
		a.CoinbaseTxID == b.CoinbaseTxID &&
		a.BlockHex == b.BlockHex
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
		var req request
		if err := json.Unmarshal(reader.Bytes(), &req); err != nil {
			_ = sess.sendResponse(response{ID: nil, Error: []any{20, "invalid json", nil}})
			continue
		}
		if err := sess.handle(ctx, req); err != nil {
			_ = sess.sendResponse(response{ID: req.ID, Error: []any{20, err.Error(), nil}})
		}
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
		sess.subscribed = true
		return sess.sendResponse(response{
			ID: req.ID,
			Result: []any{
				[][]string{{"mining.notify", sess.sessionID}, {"mining.set_difficulty", sess.sessionID}},
				sess.sessionID,
				0,
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
		if job := sess.server.currentJob(); job != nil && sess.subscribed {
			if err := sess.sendDifficulty(sess.difficulty); err != nil {
				return err
			}
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
	_ = extranonce2

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
	ntime, err := strconv.ParseUint(strings.TrimSpace(ntimeHex), 16, 32)
	if err != nil {
		sess.server.svc.RecordShare(worker, false, false, "invalid ntime")
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: []any{22, "invalid ntime", nil}})
	}
	nonce, err := strconv.ParseUint(strings.TrimSpace(nonceHex), 16, 32)
	if err != nil {
		sess.server.svc.RecordShare(worker, false, false, "invalid nonce")
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: []any{22, "invalid nonce", nil}})
	}
	if int64(ntime) < job.Template.Timestamp {
		sess.server.svc.RecordShare(worker, false, false, "ntime before template")
		return sess.sendResponse(response{ID: req.ID, Result: false, Error: []any{23, "ntime before template", nil}})
	}

	binary.LittleEndian.PutUint64(headerBytes[headerTimestampOffset:headerBitsOffset], uint64(ntime))
	binary.LittleEndian.PutUint32(headerBytes[headerBitsOffset:headerNonceOffset], job.TargetBits)
	binary.LittleEndian.PutUint32(headerBytes[headerNonceOffset:headerHeightOffset], uint32(nonce))
	copy(blockBytes[:headerLength], headerBytes[:headerLength])

	hash := blake256.Sum256(headerBytes)
	networkTarget := compactToBig(job.TargetBits)
	shareTarget := difficultyToTarget(sess.currentDifficulty())
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
	return sess.sendResponse(response{
		Method: "mining.set_difficulty",
		Params: []any{difficulty},
	})
}

func (sess *session) currentDifficulty() float64 {
	if sess.difficulty > 0 {
		return sess.difficulty
	}
	return sess.server.svc.ShareDifficulty()
}

func (sess *session) sendNotify(job *Job, clean bool) error {
	ntime := fmt.Sprintf("%08x", uint32(job.Template.Timestamp))
	return sess.sendResponse(response{
		Method: "mining.notify",
		Params: []any{
			job.ID,
			job.Template.PreviousBlockHash,
			job.Template.CoinbaseTxID,
			job.Template.Bits,
			ntime,
			clean,
			job.HeaderHex,
		},
	})
}

func (sess *session) sendResponse(resp response) error {
	sess.writerMu.Lock()
	defer sess.writerMu.Unlock()
	encoded, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = sess.conn.Write(encoded)
	return err
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

func difficultyToTarget(difficulty float64) *big.Int {
	if difficulty <= 0 {
		difficulty = 1
	}
	base := compactToBig(0x207fffff)
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
