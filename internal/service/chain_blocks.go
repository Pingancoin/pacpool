package service

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/Pingancoin/pacpool/internal/upstream"
)

func (s *Service) collectRecentChainBlocks(ctx context.Context, limit int) ([]ChainBlockRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	list, err := s.pacdata.Blocks(ctx, 1, limit)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	known := make(map[string]ChainBlockRecord, len(s.chainBlocksByHash))
	for hash, record := range s.chainBlocksByHash {
		known[hash] = cloneChainBlockRecord(record)
	}
	s.mu.RUnlock()

	records := make([]ChainBlockRecord, len(list.Entries))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 12)
	for i, block := range list.Entries {
		if cached, ok := known[block.Hash]; ok && cached.CoinbaseTxID == firstTxID(block.TxIDs) {
			records[i] = cached
			continue
		}
		wg.Add(1)
		go func(i int, block upstream.IndexedBlock) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				records[i] = s.chainBlockRecordFromIndexedBlockNoTx(block)
				return
			}
			records[i] = s.chainBlockRecordFromIndexedBlock(ctx, block)
		}(i, block)
	}
	wg.Wait()
	sort.Slice(records, func(i, j int) bool {
		return records[i].Height > records[j].Height
	})
	return records, nil
}

func (s *Service) chainBlockRecordFromIndexedBlockNoTx(block upstream.IndexedBlock) ChainBlockRecord {
	return ChainBlockRecord{
		ObservedAt:   s.now().UTC(),
		Height:       block.Height,
		Hash:         block.Hash,
		PrevHash:     block.PrevHash,
		Time:         block.Time,
		Bits:         block.Bits,
		Difficulty:   block.Difficulty,
		Nonce:        block.Nonce,
		Subsidy:      block.Subsidy,
		CoinbaseTxID: firstTxID(block.TxIDs),
	}
}

func (s *Service) chainBlockRecordFromIndexedBlock(ctx context.Context, block upstream.IndexedBlock) ChainBlockRecord {
	record := s.chainBlockRecordFromIndexedBlockNoTx(block)
	if record.CoinbaseTxID == "" {
		return record
	}
	tx, err := s.pacdata.Transaction(ctx, record.CoinbaseTxID)
	if err != nil {
		return record
	}
	for _, out := range tx.Vout {
		record.CoinbaseOutputs = append(record.CoinbaseOutputs, ChainBlockOutput{
			N:       out.N,
			Value:   out.Value,
			Address: out.Address,
		})
	}
	if len(record.CoinbaseOutputs) > 0 {
		record.MinerAddress = record.CoinbaseOutputs[0].Address
		record.MinerReward = record.CoinbaseOutputs[0].Value
	}
	if len(record.CoinbaseOutputs) > 1 {
		record.ProjectAddress = record.CoinbaseOutputs[1].Address
		record.ProjectReward = record.CoinbaseOutputs[1].Value
	}
	return record
}

func (s *Service) applyRecentChainBlocksLocked(records []ChainBlockRecord) {
	if len(records) == 0 {
		return
	}
	officialRounds := make(map[string]RoundState)
	for _, round := range s.recentRounds {
		if round.BlockHash != "" {
			officialRounds[round.BlockHash] = round
		}
	}

	toLog := make([]ChainBlockRecord, 0)
	for i := range records {
		records[i].OfficialPoolCoinbase = strings.TrimSpace(s.miningAddr) != "" && records[i].MinerAddress == s.miningAddr
		if round, ok := officialRounds[records[i].Hash]; ok {
			records[i].OfficialPoolRound = true
			records[i].OfficialPoolWorker = round.FoundBy
		}
		s.chainBlocksByHash[records[i].Hash] = cloneChainBlockRecord(records[i])
		if _, seen := s.loggedChainBlockHashes[records[i].Hash]; !seen {
			s.loggedChainBlockHashes[records[i].Hash] = struct{}{}
			toLog = append(toLog, records[i])
		}
	}
	if len(records) > 100 {
		records = records[:100]
	}
	s.recentChainBlocks = cloneChainBlockRecords(records)
	s.state.Pool.RecentChainBlocks = cloneChainBlockRecords(records)
	if len(toLog) > 0 {
		go s.appendChainBlockRecords(toLog)
	}
}

func (s *Service) appendChainBlockRecords(records []ChainBlockRecord) {
	if s.chainBlockLogPath == "" {
		return
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	file, err := os.OpenFile(s.chainBlockLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		s.setLedgerError(err)
		return
	}
	defer file.Close()
	for _, record := range records {
		encoded, err := json.Marshal(record)
		if err != nil {
			s.setLedgerError(err)
			return
		}
		encoded = append(encoded, '\n')
		if _, err := file.Write(encoded); err != nil {
			s.setLedgerError(err)
			return
		}
	}
	s.setLedgerError(nil)
}

func firstTxID(txids []string) string {
	if len(txids) == 0 {
		return ""
	}
	return strings.TrimSpace(txids[0])
}

func cloneChainBlockRecord(record ChainBlockRecord) ChainBlockRecord {
	clone := record
	clone.CoinbaseOutputs = append([]ChainBlockOutput(nil), record.CoinbaseOutputs...)
	return clone
}

func cloneChainBlockRecords(records []ChainBlockRecord) []ChainBlockRecord {
	out := make([]ChainBlockRecord, 0, len(records))
	for _, record := range records {
		out = append(out, cloneChainBlockRecord(record))
	}
	return out
}
