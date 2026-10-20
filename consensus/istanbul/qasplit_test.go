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
	"testing"

	"github.com/kaiachain/kaia/common"
	"github.com/stretchr/testify/assert"
)

func qaSplitEnv(vals map[string]string) func(string) string {
	return func(k string) string { return vals[k] }
}

func TestQASplit_disabledWithoutEnv(t *testing.T) {
	s := newQASplitState(qaSplitEnv(nil))
	assert.False(t, s.Config().Enabled)
	assert.False(t, s.Candidate(100))
	assert.False(t, s.RestrictCommit(100, 0))
	assert.False(t, s.DropBlock(100))

	// A partial or malformed environment stays disabled.
	for _, vals := range []map[string]string{
		{qaSplitHolderEnv: "0x1", qaSplitModeEnv: "holder", qaSplitMinHeightEnv: "10"},
		{qaSplitHolderEnv: common.HexToAddress("0x1").Hex(), qaSplitModeEnv: "both", qaSplitMinHeightEnv: "10"},
		{qaSplitHolderEnv: common.HexToAddress("0x1").Hex(), qaSplitModeEnv: "holder", qaSplitMinHeightEnv: "0"},
		{qaSplitHolderEnv: common.HexToAddress("0x1").Hex(), qaSplitModeEnv: "holder", qaSplitMinHeightEnv: "10", qaSplitEpochEnv: "x"},
	} {
		assert.False(t, newQASplitState(qaSplitEnv(vals)).Config().Enabled, "%v", vals)
	}
}

func TestQASplit_armsOnceAndAppliesFaults(t *testing.T) {
	var (
		n1     = common.HexToAddress("0x1")
		n2     = common.HexToAddress("0x2")
		holder = common.HexToAddress("0x3")
		n4     = common.HexToAddress("0x4")
	)
	env := map[string]string{
		qaSplitHolderEnv: holder.Hex(), qaSplitModeEnv: "holder", qaSplitMinHeightEnv: "100", qaSplitEpochEnv: "64",
	}
	s := newQASplitState(qaSplitEnv(env))
	assert.True(t, s.Config().Enabled)

	// Below the minimum, or at an epoch position outside [2, epoch-12], never.
	assert.False(t, s.ShouldArm(99, n1, n2, holder))
	assert.False(t, s.ShouldArm(128, n1, n2, holder))    // epoch start
	assert.False(t, s.ShouldArm(129, n1, n2, holder))    // H+1 is... position 1: H-1 is the start, H+10 fine but H%e=1 < 2
	assert.False(t, s.ShouldArm(128+53, n1, n2, holder)) // H+11 would be the next start
	// The holder must be neither the round-0 nor the round-1 proposer.
	assert.False(t, s.ShouldArm(130, holder, n2, holder))
	assert.False(t, s.ShouldArm(130, n1, holder, holder))
	// Run 1: the holder must propose H+1.
	assert.False(t, s.ShouldArm(130, n1, n2, n4))
	assert.True(t, s.ShouldArm(130, n1, n2, holder))

	// Arm as the holder.
	assert.True(t, s.Arm(130, holder))
	assert.False(t, s.Arm(131, holder), "a second height is refused")
	assert.Equal(t, uint64(130), s.Height())
	assert.True(t, s.IsHolder())
	assert.False(t, s.Candidate(140), "armed: no further candidates")

	assert.True(t, s.RestrictCommit(130, 0))
	assert.False(t, s.RestrictCommit(130, 1), "round 1 commits flow normally")
	assert.False(t, s.RestrictCommit(131, 0))
	assert.False(t, s.DropBlock(130), "the holder keeps the block it propagates to itself")

	// A non-holder drops the propagated block at H only.
	o := newQASplitState(qaSplitEnv(env))
	assert.True(t, o.Arm(130, n1))
	assert.False(t, o.IsHolder())
	assert.True(t, o.DropBlock(130))
	assert.False(t, o.DropBlock(129))
	assert.False(t, o.DropBlock(131))
}

func TestQASplit_holderRRMode(t *testing.T) {
	var (
		n1     = common.HexToAddress("0x1")
		holder = common.HexToAddress("0x3")
		n4     = common.HexToAddress("0x4")
	)
	s := newQASplitState(qaSplitEnv(map[string]string{
		qaSplitHolderEnv: holder.Hex(), qaSplitModeEnv: "holder-rr", qaSplitMinHeightEnv: "10",
	}))
	assert.True(t, s.Config().Enabled)
	// RoundRobin: the proposer of H+1 is the round-1 proposer of H, so n2 == holder is allowed.
	assert.True(t, s.ShouldArm(10, n1, holder, holder))
	assert.False(t, s.ShouldArm(10, holder, n1, holder), "the holder must not propose H itself")
	assert.False(t, s.ShouldArm(10, n1, holder, n4), "the holder must propose H+1")
}

func TestQASplit_otherRRMode(t *testing.T) {
	var (
		n1     = common.HexToAddress("0x1")
		n2     = common.HexToAddress("0x2")
		holder = common.HexToAddress("0x3")
	)
	s := newQASplitState(qaSplitEnv(map[string]string{
		qaSplitHolderEnv: holder.Hex(), qaSplitModeEnv: "other-rr", qaSplitMinHeightEnv: "10",
	}))
	// RoundRobin: N2 proposes H+1; the holder must be neither N1 nor N2.
	assert.True(t, s.ShouldArm(10, n1, n2, n2))
	assert.False(t, s.ShouldArm(10, holder, n2, n2))
	assert.False(t, s.ShouldArm(10, n1, holder, holder))
}

func TestQASplit_otherMode(t *testing.T) {
	var (
		n1     = common.HexToAddress("0x1")
		n2     = common.HexToAddress("0x2")
		holder = common.HexToAddress("0x3")
		n4     = common.HexToAddress("0x4")
	)
	s := newQASplitState(qaSplitEnv(map[string]string{
		qaSplitHolderEnv: holder.Hex(), qaSplitModeEnv: "other", qaSplitMinHeightEnv: "10",
	}))
	// Run 2: the fourth node must propose H+1.
	assert.True(t, s.ShouldArm(10, n1, n2, n4))
	assert.False(t, s.ShouldArm(10, n1, n2, holder))
	assert.False(t, s.ShouldArm(10, n1, n2, n1))
	assert.False(t, s.ShouldArm(10, n1, n2, n2))
	// No epoch window configured: any height at or past the minimum qualifies.
	assert.True(t, s.Candidate(64))
}
