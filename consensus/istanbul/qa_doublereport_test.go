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
	"errors"
	"testing"

	"github.com/kaiachain/kaia/common"
	"github.com/stretchr/testify/assert"
)

func TestQADoubleReportDisabled(t *testing.T) {
	s := newQADoubleReportState(func(string) string { return "" })
	assert.False(t, s.Config().Enabled)
	k, err := s.Ordinal(10, func(uint64, uint64) (common.Address, error) {
		t.Fatal("must not be called")
		return common.Address{}, nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 0, k)

	// Half-configured stays disabled.
	s = newQADoubleReportState(func(k string) string {
		if k == qaDoubleReportTargetEnv {
			return "0x0000000000000000000000000000000000000001"
		}
		return ""
	})
	assert.False(t, s.Config().Enabled)
}

func TestQADoubleReportOrdinal(t *testing.T) {
	n1 := common.HexToAddress("0x1")
	n2 := common.HexToAddress("0x2")
	// Round-0 proposer per height: N1 at 11, 13, 17 and 21. Start at 12, so "7" = 13 and "9" = 17.
	sched := map[uint64]common.Address{11: n1, 12: n2, 13: n1, 14: n2, 15: n2, 16: n2, 17: n1, 18: n2, 19: n2, 20: n2, 21: n1}
	calls := 0
	getProposer := func(num, round uint64) (common.Address, error) {
		calls++
		assert.Equal(t, uint64(0), round)
		p, ok := sched[num]
		if !ok {
			return common.Address{}, errors.New("no header")
		}
		return p, nil
	}
	s := newQADoubleReportState(func(k string) string {
		switch k {
		case qaDoubleReportTargetEnv:
			return n1.Hex()
		case qaDoubleReportStartEnv:
			return "12"
		}
		return ""
	})
	assert.True(t, s.Config().Enabled)

	for num, want := range map[uint64]int{11: 0, 12: 0, 13: 1, 14: 0, 16: 0, 17: 2, 18: 0, 21: 0} {
		k, err := s.Ordinal(num, getProposer)
		assert.NoError(t, err, num)
		assert.Equal(t, want, k, num)
	}
	// Once both heights are cached, no further proposer lookups happen.
	calls = 0
	for _, num := range []uint64{13, 17, 21, 100} {
		_, err := s.Ordinal(num, getProposer)
		assert.NoError(t, err)
	}
	assert.Equal(t, 0, calls)

	// A lookup error inside the walk is reported; what was found before it stays cached.
	delete(sched, 15)
	s = newQADoubleReportState(func(k string) string {
		switch k {
		case qaDoubleReportTargetEnv:
			return n1.Hex()
		case qaDoubleReportStartEnv:
			return "12"
		}
		return ""
	})
	_, err := s.Ordinal(17, getProposer) // height 15 has no header
	assert.Error(t, err)
	k, err := s.Ordinal(13, getProposer)
	assert.NoError(t, err)
	assert.Equal(t, 1, k)
	sched[15] = n2
	k, err = s.Ordinal(17, getProposer)
	assert.NoError(t, err)
	assert.Equal(t, 2, k)
}
