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
	"testing"
	"time"

	"github.com/kaiachain/kaia/common"
	blstypes "github.com/kaiachain/kaia/crypto/bls/types"
	"github.com/kaiachain/kaia/kaiax/vrank"
)

func TestQAVRankDuplicateReplayCount(t *testing.T) {
	t.Run("disabled without replay count", func(t *testing.T) {
		t.Setenv(qaVRankDuplicateReplaysEnv, "")
		t.Setenv(qaVRankReplayBlockEnv, "3")
		if _, ok := qaVRankDuplicateReplayCount(3); ok {
			t.Fatal("QA replay hook should be disabled without a replay count")
		}
	})

	t.Run("disabled without replay block", func(t *testing.T) {
		t.Setenv(qaVRankDuplicateReplaysEnv, "10")
		t.Setenv(qaVRankReplayBlockEnv, "")
		if _, ok := qaVRankDuplicateReplayCount(3); ok {
			t.Fatal("QA replay hook should be disabled without a replay block")
		}
	})

	t.Run("enabled only for configured block", func(t *testing.T) {
		t.Setenv(qaVRankDuplicateReplaysEnv, "10")
		t.Setenv(qaVRankReplayBlockEnv, "3")

		if count, ok := qaVRankDuplicateReplayCount(2); ok || count != 0 {
			t.Fatalf("unexpected replay for block 2: count=%d ok=%t", count, ok)
		}
		if count, ok := qaVRankDuplicateReplayCount(3); !ok || count != 10 {
			t.Fatalf("unexpected replay configuration: count=%d ok=%t", count, ok)
		}
		if !qaVRankDuplicateReplayEnabled(3) || qaVRankDuplicateReplayEnabled(4) {
			t.Fatal("unexpected replay-enabled state")
		}
	})
}

func TestMaybeBroadcastQADuplicateVRankCandidates(t *testing.T) {
	t.Setenv(qaVRankDuplicateReplaysEnv, "3")
	t.Setenv(qaVRankReplayBlockEnv, "7")

	v := NewVRankModule()
	proposer := common.HexToAddress("0x1234")
	candidate := &vrank.VRankCandidate{
		BlockNumber: 7,
		Round:       1,
		BlsSig:      [blstypes.SignatureLength]byte{1},
	}
	v.maybeBroadcastQADuplicateVRankCandidates(candidate, proposer)

	for i := 0; i < 3; i++ {
		select {
		case event := <-v.broadcastCh:
			if len(event.Targets) != 1 || event.Targets[0] != proposer {
				t.Fatalf("unexpected replay targets: %v", event.Targets)
			}
			replay, ok := event.Msg.(*vrank.VRankCandidate)
			if !ok {
				t.Fatalf("unexpected replay type: %T", event.Msg)
			}
			if replay.BlsSig != ([blstypes.SignatureLength]byte{}) {
				t.Fatal("QA replay must contain an invalid zero BLS signature")
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for QA replay")
		}
	}

	if candidate.BlsSig == ([blstypes.SignatureLength]byte{}) {
		t.Fatal("QA replay must not alter the initial valid candidate")
	}
}
