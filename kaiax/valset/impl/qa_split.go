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

package impl

import (
	"math/big"

	"github.com/kaiachain/kaia/blockchain/types"
	"github.com/kaiachain/kaia/common"
)

// QANextProposer returns the validator scheduled to propose {num+1, round 0}
// if `proposed` becomes block num, where num is not yet in the chain. It is
// used only by the round-split QA fault injection (consensus/istanbul/qasplit.go)
// to pick the split height before block num is committed.
//
// It assumes the qualified set of num+1 equals that of num, which holds on a
// network whose validators do not change at num+1. Under WeightedRandom with
// Randao the proposer of num+1 depends only on proposed.MixHash, which the block
// hash covers, so every node computes the same answer from the same proposal.
func (v *ValsetModule) QANextProposer(proposed *types.Header) (common.Address, error) {
	num := proposed.Number.Uint64()
	c, err := v.getBlockContext(num)
	if err != nil {
		return common.Address{}, err
	}
	author, err := v.Chain.Sealer().Author(proposed)
	if err != nil {
		return common.Address{}, err
	}
	next := &blockContext{
		num:          num + 1,
		qualified:    c.qualified,
		prevHeader:   proposed,
		prevProposer: author,
		rules:        v.Chain.Config().Rules(new(big.Int).SetUint64(num + 1)),
		pset:         c.pset, // the proposer policy does not change between num and num+1
	}
	return v.getProposer(next, 0)
}
