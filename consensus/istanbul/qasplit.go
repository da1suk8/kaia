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
	"sync/atomic"

	"github.com/kaiachain/kaia/common"
)

// QA-only fault injection for the "round split at height H" scenario
// (kaiachain/kaia issue 990, RC3 section). Disabled unless the environment
// variables below are set; a release binary never reads them.
//
// The scenario: N1 proposes block B at {H, round 0}. Every node sends its
// round-0 COMMIT only to one node, the holder (N3), which therefore commits B
// at round 0. The other nodes drop the block the holder propagates, change
// round, and commit the same B at round 1 through the round-change
// certificate (N2 re-proposes it). All four nodes then hold one block with
// one hash but two different rounds, and H+1 must reconcile them through
// header.VRank (PR 1054).
//
//	KAIA_QA_SPLIT_HOLDER      address of the node that commits at round 0 (N3)
//	KAIA_QA_SPLIT_MODE        "holder":    H is chosen so that the holder proposes H+1 (run 1; needs a
//	                                       policy where the proposer of H+1 is independent of the
//	                                       round-1 proposer of H, i.e. WeightedRandom + Randao)
//	                          "holder-rr": like "holder" but the holder may be the round-1 proposer of H
//	                                       (RoundRobin: the proposer of H+1 always is), so round 1 of H
//	                                       has no proposer and the other nodes commit B at round 2
//	                          "other":     H is chosen so that the fourth node proposes H+1 (run 2)
//	                          "other-rr":  run 2 under RoundRobin: the round-1 proposer of H proposes
//	                                       H+1 (it always does), the holder is neither N1 nor N2
//	KAIA_QA_SPLIT_MIN_HEIGHT  the first height at which H may be chosen
//	KAIA_QA_SPLIT_EPOCH       optional: VRank epoch length; H is chosen only when
//	                          H % epoch is in [2, epoch-12], so that neither H nor
//	                          H+1 is an epoch start and H+10 is inside the epoch
//
// Every node evaluates the same PRE-PREPARE for {H, 0} with the same chain
// state, so all of them arm at the same H without coordination.

const (
	qaSplitHolderEnv    = "KAIA_QA_SPLIT_HOLDER"
	qaSplitModeEnv      = "KAIA_QA_SPLIT_MODE"
	qaSplitMinHeightEnv = "KAIA_QA_SPLIT_MIN_HEIGHT"
	qaSplitEpochEnv     = "KAIA_QA_SPLIT_EPOCH"

	QASplitModeHolder   = "holder"
	QASplitModeHolderRR = "holder-rr"
	QASplitModeOther    = "other"
	QASplitModeOtherRR  = "other-rr"
)

// QASplitConfig is the parsed environment. Enabled is false unless every
// required variable is present and valid.
type QASplitConfig struct {
	Enabled   bool
	Holder    common.Address
	Mode      string
	MinHeight uint64
	Epoch     uint64
}

type qaSplitState struct {
	cfg    QASplitConfig
	height atomic.Uint64 // 0 = not armed
	holder atomic.Bool   // this node is the holder (set when armed)
}

// QASplit is the process-wide state shared by the consensus core (arms it),
// the istanbul backend (restricts COMMITs) and the CN protocol handler
// (drops the propagated block).
var QASplit = newQASplitState(os.Getenv)

func newQASplitState(getenv func(string) string) *qaSplitState {
	return &qaSplitState{cfg: parseQASplitConfig(getenv)}
}

func parseQASplitConfig(getenv func(string) string) QASplitConfig {
	var cfg QASplitConfig
	holder := strings.TrimSpace(getenv(qaSplitHolderEnv))
	if !common.IsHexAddress(holder) {
		return cfg
	}
	mode := strings.ToLower(strings.TrimSpace(getenv(qaSplitModeEnv)))
	if mode != QASplitModeHolder && mode != QASplitModeHolderRR && mode != QASplitModeOther && mode != QASplitModeOtherRR {
		return cfg
	}
	minHeight, err := strconv.ParseUint(strings.TrimSpace(getenv(qaSplitMinHeightEnv)), 10, 64)
	if err != nil || minHeight == 0 {
		return cfg
	}
	var epoch uint64
	if s := strings.TrimSpace(getenv(qaSplitEpochEnv)); s != "" {
		epoch, err = strconv.ParseUint(s, 10, 64)
		if err != nil {
			return cfg
		}
	}
	return QASplitConfig{Enabled: true, Holder: common.HexToAddress(holder), Mode: mode, MinHeight: minHeight, Epoch: epoch}
}

// Config returns the parsed environment.
func (s *qaSplitState) Config() QASplitConfig { return s.cfg }

// Height returns the armed height H, or 0 when not armed.
func (s *qaSplitState) Height() uint64 { return s.height.Load() }

// IsHolder reports whether this node is the round-0 holder (valid once armed).
func (s *qaSplitState) IsHolder() bool { return s.holder.Load() }

// Candidate reports whether height num at round 0 may become H: the fault is
// enabled, not yet armed, num is at or past the minimum and inside the epoch
// window.
func (s *qaSplitState) Candidate(num uint64) bool {
	if !s.cfg.Enabled || s.height.Load() != 0 || num < s.cfg.MinHeight {
		return false
	}
	if e := s.cfg.Epoch; e != 0 {
		if e < 16 {
			return false
		}
		pos := num % e
		if pos < 2 || pos > e-12 {
			return false
		}
	}
	return true
}

// ShouldArm decides whether the PRE-PREPARE for {num, 0} proposed by n1 makes
// num the split height. n2 is the scheduled proposer of {num, 1} and next the
// scheduled proposer of {num+1, 0} given this proposal.
//
// Both runs need the holder to be neither N1 (it must commit someone else's
// block) nor N2 (it is already at H+1 when round 1 starts). Run 1 ("holder")
// needs the holder to propose H+1; run 2 ("other") needs the remaining node to.
func (s *qaSplitState) ShouldArm(num uint64, n1, n2, next common.Address) bool {
	if !s.Candidate(num) {
		return false
	}
	h := s.cfg.Holder
	if s.cfg.Mode == QASplitModeHolderRR {
		// The holder leaves H as soon as it commits at round 0, so if it is also the
		// round-1 proposer, round 1 times out and B commits at round 2 on the others.
		return n1 != h && next == h
	}
	if n1 == h || n2 == h || n1 == n2 {
		return false
	}
	switch s.cfg.Mode {
	case QASplitModeHolder:
		return next == h
	case QASplitModeOther:
		return next != h && next != n1 && next != n2
	case QASplitModeOtherRR:
		return next == n2
	}
	return false
}

// Arm records H and whether this node is the holder. It returns false when
// another height was armed first.
func (s *qaSplitState) Arm(num uint64, self common.Address) bool {
	if !s.height.CompareAndSwap(0, num) {
		return false
	}
	s.holder.Store(self == s.cfg.Holder)
	return true
}

// RestrictCommit reports whether a COMMIT for the view {seq, round} must be
// delivered to the holder only.
func (s *qaSplitState) RestrictCommit(seq, round uint64) bool {
	h := s.height.Load()
	return h != 0 && seq == h && round == 0
}

// DropBlock reports whether a block of height num arriving through block
// propagation (NewBlock / NewBlockHashes) must be ignored. Only the holder
// commits H at round 0; every other node must receive H through consensus.
func (s *qaSplitState) DropBlock(num uint64) bool {
	h := s.height.Load()
	return h != 0 && num == h && !s.holder.Load()
}
