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
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A node that was never armed must never skip.
func TestProposalSkip_DisarmedByDefault(t *testing.T) {
	sb := &backend{}
	assert.Equal(t, int64(0), sb.ProposalSkips())
	assert.False(t, sb.ConsumeProposalSkip())
}

// SetRoundChange(count) yields exactly count skips, then stops.
func TestProposalSkip_ConsumesExactlyCount(t *testing.T) {
	for _, count := range []int64{1, 2, 5} {
		sb := &backend{}
		sb.SetProposalSkips(count)
		assert.Equal(t, count, sb.ProposalSkips())

		for i := int64(0); i < count; i++ {
			assert.True(t, sb.ConsumeProposalSkip(), "skip %d of %d", i+1, count)
		}
		assert.False(t, sb.ConsumeProposalSkip(), "count=%d: must stop after %d skips", count, count)
		assert.Equal(t, int64(0), sb.ProposalSkips())
	}
}

// Passing 0 disarms a pending count; negatives are clamped rather than
// underflowing into a permanent skip.
func TestProposalSkip_Disarm(t *testing.T) {
	sb := &backend{}
	sb.SetProposalSkips(3)
	sb.SetProposalSkips(0)
	assert.False(t, sb.ConsumeProposalSkip())

	sb.SetProposalSkips(-1)
	assert.Equal(t, int64(0), sb.ProposalSkips())
	assert.False(t, sb.ConsumeProposalSkip())
}

// The counter is read from the consensus goroutine while the RPC writes it, so
// concurrent consumers must not hand out more skips than were armed.
func TestProposalSkip_ConcurrentConsumeDoesNotOverrun(t *testing.T) {
	const (
		armed   = 50
		callers = 16
		each    = 20
	)
	sb := &backend{}
	sb.SetProposalSkips(armed)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		granted int
	)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := 0
			for j := 0; j < each; j++ {
				if sb.ConsumeProposalSkip() {
					local++
				}
			}
			mu.Lock()
			granted += local
			mu.Unlock()
		}()
	}
	wg.Wait()

	assert.Equal(t, armed, granted, "must grant exactly the armed number of skips")
	assert.Equal(t, int64(0), sb.ProposalSkips())
}

// The API wrapper returns the value it stored, so callers can confirm the arm.
func TestSetRoundChangeAPI(t *testing.T) {
	sb := &backend{}
	api := &API{istanbul: sb}

	assert.Equal(t, int64(2), api.SetRoundChange(2))
	assert.Equal(t, int64(2), api.GetRoundChange())

	assert.True(t, sb.ConsumeProposalSkip())
	assert.Equal(t, int64(1), api.GetRoundChange())

	assert.Equal(t, int64(0), api.SetRoundChange(0))
	assert.False(t, sb.ConsumeProposalSkip())
}
