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

package core

import (
	"github.com/kaiachain/kaia/blockchain/types"
	"github.com/kaiachain/kaia/common"
	"github.com/kaiachain/kaia/consensus/bft"
	"github.com/kaiachain/kaia/consensus/istanbul"
)

// qaNextProposerReader is implemented by kaiax/valset/impl.ValsetModule for the
// round-split QA only; the valset interface itself is untouched.
type qaNextProposerReader interface {
	QANextProposer(proposed *types.Header) (common.Address, error)
}

// qaSplitMaybeArm is called once a round-0 PRE-PREPARE has passed every
// check. It decides, identically on every node, whether this height is the
// split height H (see consensus/istanbul/qasplit.go).
func (c *core) qaSplitMaybeArm(preprepare *bft.Preprepare, src common.Address) {
	s := istanbul.QASplit
	seq := preprepare.View.Sequence.Uint64()
	if preprepare.View.Round.Sign() != 0 || !s.Candidate(seq) {
		return
	}
	block, ok := preprepare.Proposal.(*types.Block)
	if !ok || c.valsetModule == nil {
		return
	}
	reader, ok := c.valsetModule.(qaNextProposerReader)
	if !ok {
		c.logger.Error("QA split: valset module cannot compute the next proposer")
		return
	}
	n2, err := c.valsetModule.GetProposer(seq, 1)
	if err != nil {
		c.logger.Error("QA split: GetProposer(H, 1) failed", "num", seq, "err", err)
		return
	}
	next, err := reader.QANextProposer(block.Header())
	if err != nil {
		c.logger.Error("QA split: next proposer failed", "num", seq, "err", err)
		return
	}
	if !s.ShouldArm(seq, src, n2, next) {
		c.logger.Info("QA split: height not eligible", "num", seq, "n1", src, "n2", n2, "next", next, "holder", s.Config().Holder, "mode", s.Config().Mode)
		return
	}
	if !s.Arm(seq, c.Address()) {
		return
	}
	c.logger.Warn("QA split: armed", "H", seq, "mode", s.Config().Mode, "n1", src, "n2", n2, "nextProposer", next,
		"holder", s.Config().Holder, "thisNodeIsHolder", s.IsHolder(), "hash", block.Hash())
}
