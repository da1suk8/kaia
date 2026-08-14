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
	"os"
	"strconv"

	"github.com/kaiachain/kaia/common"
	blstypes "github.com/kaiachain/kaia/crypto/bls/types"
	"github.com/kaiachain/kaia/kaiax/vrank"
)

const (
	qaVRankDuplicateReplaysEnv = "KAIA_QA_VRANK_DUPLICATE_REPLAYS"
	qaVRankReplayBlockEnv      = "KAIA_QA_VRANK_REPLAY_BLOCK"
)

// maybeBroadcastQADuplicateVRankCandidates sends intentionally-invalid BLS replays after a
// valid candidate. It is a local QA hook, disabled unless both environment variables are set.
//
// KAIA_QA_VRANK_DUPLICATE_REPLAYS is the number of replays to emit, and
// KAIA_QA_VRANK_REPLAY_BLOCK is the single block number at which to emit them.
func (v *VRankModule) maybeBroadcastQADuplicateVRankCandidates(candidate *vrank.VRankCandidate, proposer common.Address) {
	count, ok := qaVRankDuplicateReplayCount(candidate.BlockNumber)
	if !ok {
		return
	}

	replay := *candidate
	// Keep the ECDSA signature, which identifies this candidate, but make BLS verification fail.
	// A patched receiver must discard this replay as a duplicate before attempting BLS verification.
	replay.BlsSig = [blstypes.SignatureLength]byte{}

	logger.Info("Broadcasting QA duplicate VRankCandidate replays", "blockNum", candidate.BlockNumber, "round", candidate.Round, "count", count)
	for i := 0; i < count; i++ {
		v.BroadcastVRankCandidate(&replay, proposer)
	}
}

func qaVRankDuplicateReplayCount(blockNum uint64) (int, bool) {
	count, err := strconv.Atoi(os.Getenv(qaVRankDuplicateReplaysEnv))
	if err != nil || count <= 0 {
		return 0, false
	}

	replayBlock, err := strconv.ParseUint(os.Getenv(qaVRankReplayBlockEnv), 10, 64)
	if err != nil || replayBlock != blockNum {
		return 0, false
	}
	return count, true
}

func qaVRankDuplicateReplayEnabled(blockNum uint64) bool {
	_, ok := qaVRankDuplicateReplayCount(blockNum)
	return ok
}
