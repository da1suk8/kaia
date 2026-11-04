// Copyright 2026 The Kaia Authors
// This file is part of the Kaia library.
//
// The Kaia library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The Kaia library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the Kaia library. If not, see <http://www.gnu.org/licenses/>.

package istanbul

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/kaiachain/kaia/common"
)

// QA-only fault injection for the "no double report after a re-proposal"
// scenario (kaiachain/kaia issue 990, RC4 section, PR 1104). Disabled unless
// both environment variables below are set; a release binary never reads them.
//
// The scenario needs three proposals of one validator N1 in one VRank epoch:
//
//	"7": N1 proposes and commits at round 0; the candidate C1 does not answer
//	     N1's VRankPreprepare, so N1's next header reports [C1].
//	"9": N1 proposes at round 0; every validator locks the block but withholds
//	     its round-0 COMMIT, so N2 re-proposes the locked block at round 1.
//	     header(9).VRank is the one N1 built: report [C1] on "7".
//	"N": N1's next proposal. Its report must be empty (rc.3 repeats [C1]).
//
// Under WeightedRandom + Randao the heights are not known in advance, so the
// fault keys on N1's turn instead of a fixed height: "7" is the first height
// at or above KAIA_QA_START_HEIGHT whose round-0 proposer is N1, "9" the
// second. Every node derives the round-0 proposer of a height from the same
// committed parent (valset GetProposer), so all of them pick the same heights
// without coordination.
//
//	KAIA_QA_TARGET_PROPOSER  address of N1
//	KAIA_QA_START_HEIGHT     first height that may become "7" (> 0)
//	KAIA_QA_TRACE=1          extra Info logs on every node (solicit / reply / report target)
//
// The candidate (kaiax/vrank HandleVRankPreprepare) stays silent at the first
// height; every validator (istanbul core broadcastCommit) drops its round-0
// COMMIT at the second height.

const (
	qaDoubleReportTargetEnv = "KAIA_QA_TARGET_PROPOSER"
	qaDoubleReportStartEnv  = "KAIA_QA_START_HEIGHT"
	qaTraceEnv              = "KAIA_QA_TRACE"
)

// QADoubleReportConfig is the parsed environment. Enabled is false unless both
// variables are present and valid.
type QADoubleReportConfig struct {
	Enabled     bool
	Target      common.Address
	StartHeight uint64
}

type qaDoubleReportState struct {
	cfg QADoubleReportConfig

	mu     sync.Mutex
	first  uint64 // "7": first height >= StartHeight proposed by Target at round 0 (0 = unknown)
	second uint64 // "9": second such height (0 = unknown)
}

// QADoubleReport is the process-wide state shared by the istanbul core
// (withholds the round-0 COMMIT) and the vrank module (candidate silence).
var QADoubleReport = newQADoubleReportState(os.Getenv)

func newQADoubleReportState(getenv func(string) string) *qaDoubleReportState {
	return &qaDoubleReportState{cfg: parseQADoubleReportConfig(getenv)}
}

func parseQADoubleReportConfig(getenv func(string) string) QADoubleReportConfig {
	var cfg QADoubleReportConfig
	target := strings.TrimSpace(getenv(qaDoubleReportTargetEnv))
	if !common.IsHexAddress(target) {
		return cfg
	}
	start, err := strconv.ParseUint(strings.TrimSpace(getenv(qaDoubleReportStartEnv)), 10, 64)
	if err != nil || start == 0 {
		return cfg
	}
	return QADoubleReportConfig{Enabled: true, Target: common.HexToAddress(target), StartHeight: start}
}

// QATrace reports whether KAIA_QA_TRACE=1 is set.
func QATrace() bool { return os.Getenv(qaTraceEnv) == "1" }

// Config returns the parsed environment.
func (s *qaDoubleReportState) Config() QADoubleReportConfig { return s.cfg }

// Ordinal reports which of the scenario's heights num is: 1 for "7", 2 for
// "9", 0 for any other height or when the fault is disabled. getProposer is
// the valset GetProposer of the calling module; it is asked for round 0 of
// every height from StartHeight to num, which is deterministic once the
// parent of num is committed. The two heights are cached once found, so the
// walk stops as soon as "9" is known.
func (s *qaDoubleReportState) Ordinal(num uint64, getProposer func(num, round uint64) (common.Address, error)) (int, error) {
	if !s.cfg.Enabled || num < s.cfg.StartHeight {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.first == num:
		return 1, nil
	case s.second == num:
		return 2, nil
	case s.second != 0:
		return 0, nil // both heights known and num is neither
	}
	found := 0
	for h := s.cfg.StartHeight; h <= num; h++ {
		p, err := getProposer(h, 0)
		if err != nil {
			return 0, err
		}
		if p != s.cfg.Target {
			continue
		}
		found++
		if found == 1 && s.first == 0 {
			s.first = h
		}
		if found == 2 {
			if s.second == 0 {
				s.second = h
			}
			break
		}
	}
	switch num {
	case s.first:
		return 1, nil
	case s.second:
		return 2, nil
	}
	return 0, nil
}
