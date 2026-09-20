package impl

import (
	"fmt"
	"testing"

	"github.com/kaiachain/kaia/blockchain/types"
	"github.com/kaiachain/kaia/common"
	"github.com/kaiachain/kaia/kaiax/vrank"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The checkpoint interval is VRankEpoch/8. When it does not divide VRankEpoch, the
// checkpoint before an epoch-start block belongs to the previous epoch; seeding from it
// carried that epoch's CP matrix and PFS forward, so a candidate new to the epoch was never
// scored ("cfReport contains address not in candidates list; skipping") and always passed,
// while the other scores never reset. Seen on a devnet with VRankEpoch 300.
func runEpochSeedCase(t *testing.T, epoch uint64) map[common.Address]uint64 {
	interval := epoch / 8
	cp := ((epoch - 1) / interval) * interval // last checkpoint block written before the epoch start
	P1, Cold, Cnew := numToAddr(1), numToAddr(10), numToAddr(11)

	headers := map[uint64]*types.Header{}
	for n := cp + 1; n < epoch; n++ { // tail of the previous epoch: Cold keeps failing
		headers[n] = makeHeaderWithVRank(n, 0, []common.Address{Cold})
	}
	headers[epoch] = makeHeaderWithRound(epoch, 0) // epoch start: VRank carries this epoch's candidate list
	headers[epoch].VRank = makeEpochStartVRankHeader(t, epoch, []common.Address{Cnew}).VRank
	for n := epoch + 1; n <= epoch+5; n++ { // Cnew fails in 5 blocks
		headers[n] = makeHeaderWithVRank(n, 0, []common.Address{Cnew})
	}
	cn := newCN(t, withHeaders(headers), withProposer(P1), withCandidates([]common.Address{Cnew}), withoutStart())
	cn.VRankModule.ChainConfig.VRankEpoch = epoch
	// What PostInsertBlock(cp) persisted in the previous epoch: a matrix that knows only Cold.
	WriteCheckpoint(cn.DB, cp, map[common.Address]uint64{}, vrank.CPMatrix{Cold: {P1: 5}})
	WriteLastCheckpoint(cn.DB, cp)

	for n := epoch; n <= epoch+5; n++ { // what a running node does at every insert
		require.NoError(t, cn.VRankModule.PostInsertBlock(types.NewBlockWithHeader(headers[n])))
	}
	cfs, err := cn.VRankModule.GetCFS(epoch + 5)
	require.NoError(t, err)
	t.Logf("epoch=%d interval=%d previous checkpoint=%d (epoch start itself is a checkpoint block=%v) -> CFS(%d) = %v", epoch, interval, cp, epoch%interval == 0, epoch+5, cfs)
	return cfs
}

// Every epoch length must reset the scores at the epoch start, whether or not
// VRankEpoch/8 divides it. Before the clamp, 60, 100 and 300 seeded the epoch
// from the previous epoch's checkpoint and never scored the new candidate.
func TestEpochSeedIgnoresPreviousEpochCheckpoint(t *testing.T) {
	want := map[common.Address]uint64{numToAddr(11): 5}
	for _, epoch := range []uint64{30, 60, 100, 120, 200, 300, 304, 320, 360, 600} {
		t.Run(fmt.Sprintf("epoch %d", epoch), func(t *testing.T) {
			assert.Equal(t, want, runEpochSeedCase(t, epoch), "epoch %d (interval %d)", epoch, epoch/8)
		})
	}
}
