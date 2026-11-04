# No double report after a re-proposal (issue 990, RC4 QA)

Local five-node network and fault injection for the RC4 item "No double report after a
re-proposal (#1104)" of kaiachain/kaia issue 990. QA only, not for merging: the `KAIA_QA_*`
environment variables gate every change in `kcn` and `homi`; a process started without them
behaves as the unpatched build.

## Fault

Every node runs with the same two variables; the roles follow from the chain.

- `KAIA_QA_TARGET_PROPOSER` = N1's address, `KAIA_QA_START_HEIGHT` = X
- "7" = the first height >= X whose round-0 proposer is N1: the candidate ignores N1's
  `VRankPreprepare` (`kaiax/vrank/impl/handler.go`), so N1's next header reports it
- "9" = the second such height: every validator withholds its round-0 COMMIT, including
  self-delivery (`consensus/istanbul/core/commit.go`), so the locked block is re-proposed at round 1
- both heights come from valset `GetProposer(h, 0)` on the committed chain
  (`consensus/istanbul/qa_doublereport.go`), so all nodes pick the same ones without coordination
- `KAIA_QA_TRACE=1` adds `QA solicit` / `QA candidate reply` / `QA report target` Info lines
- `KAIA_QA_CANDIDATE_LAST=1` makes `homi` start the fifth node as `CandTesting` (`cmd/homi/setup/cmd.go`)

N is always "9"+1: any PFS > 0 moves the failed proposer to `ValPaused` in the block after "9"
(`--pfs-threshold` only prevents `ValExiting`), so "9"+1 is N1's only possible next proposal in
the epoch. The RANDAO proposer order is fixed by the genesis, so choose N1 and X such that "9"+1
is N1's turn. With the fixture below, cn4 proposes 9, 15 and 16: N1 = cn4 and X = 9 give
"7" = 9, "9" = 15, N = 16 (the script defaults).

## Run

```sh
make kcn homi
go build -o build/bin/vrankdump ./build/qa/vrankdump
cd build/qa/no-double-report
export BIN=$PWD/../../bin OUT=/path/to/run
./net.sh fixture      # homi (KAIA_QA_CANDIDATE_LAST=1) + the jq adjustments of the RC3 comment of issue 990
./net.sh start        # QA_TARGET=cn4 QA_START=9 unless overridden; p2p 34001-34005, RPC 8701-8705
./net.sh status       # wait until the heads pass 17 (about 20 s)
./collect.sh          # writes OUT/evidence.txt
./net.sh stop
```

`collect.sh` detects "7", "9" and N from the logs and the chain, then records: the committing
round and proposers of every height, the decoded `header.VRank` of "7", "9", "9"+1 and N, the
node states per height (`kaia_getNodesByState`, needs `--gcmode archive`, which `net.sh` sets),
the candidate's silent / reply lines, every validator's `QA drop COMMIT` line, N1's
`QA report target` lines, the number of `Dropping PrepreparedEvent due to full relay queue`
lines in N1's log, and N1's process starts.

Expected on rc.4: `header("9").VRank` report = [C1], `header(N).VRank` report = [], N1 logs no
relay drop. The same diff applies to rc.3 (`e6fd426d0`), where `header(N).VRank` reports [C1] again.
