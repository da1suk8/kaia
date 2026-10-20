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

package backend

import (
	"github.com/kaiachain/kaia/common"
	"github.com/kaiachain/kaia/consensus/bft"
	"github.com/kaiachain/kaia/consensus/istanbul"
	"github.com/kaiachain/kaia/rlp"
)

// qaSplitFilterTargets narrows the receivers of a consensus message for the
// round-split QA (consensus/istanbul/qasplit.go): a COMMIT for {H, round 0},
// whether this node's own or one it relays, goes to the holder only. Every
// other message, and every COMMIT of another view, is untouched.
func (sb *backend) qaSplitFilterTargets(targets map[common.Address]bool, payload []byte) map[common.Address]bool {
	s := istanbul.QASplit
	if s.Height() == 0 || len(targets) == 0 {
		return targets
	}
	var msg bft.Message
	if err := rlp.DecodeBytes(payload, &msg); err != nil || msg.Code != bft.MsgCommit {
		return targets
	}
	var commit *bft.Commit
	if err := msg.Decode(&commit); err != nil || commit == nil || commit.View == nil {
		return targets
	}
	if !s.RestrictCommit(commit.View.Sequence.Uint64(), commit.View.Round.Uint64()) {
		return targets
	}
	holder := s.Config().Holder
	filtered := make(map[common.Address]bool, 1)
	if targets[holder] {
		filtered[holder] = true
	}
	sb.logger.Warn("QA split: round-0 COMMIT delivered to the holder only", "seq", commit.View.Sequence, "from", msg.Address,
		"holder", holder, "sentTo", len(filtered), "wouldHaveSentTo", len(targets))
	return filtered
}
