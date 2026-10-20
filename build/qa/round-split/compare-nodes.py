#!/usr/bin/env python3
"""compare-nodes.py H [--ens]  : compare what every node holds for H, H+1, H+2.

Per node and block: hash, round, proposer, originProposer, committers,
stateRoot, qualified validators (istanbul_getValidators), node states
(kaia_getNodesByState), PFS (governance_getPFS). Then a verdict line per field:
"same" when every node agrees, otherwise the differing values. At H the round
and the committers are expected to differ (that is the split); everything else
must agree on every node.
"""
import json, sys, urllib.request

CN = {"cn1": 8561, "cn2": 8562, "cn3": 8563, "cn4": 8564}
EN = {"en1": 8551, "en2": 8552, "en3": 8553}

def rpc(port, method, params):
    req = urllib.request.Request(f"http://127.0.0.1:{port}", data=json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode(),
                                 headers={"content-type": "application/json"})
    try:
        r = json.load(urllib.request.urlopen(req, timeout=5))
    except Exception as e:
        return {"error": str(e)}
    return r.get("result") if "error" not in r else {"error": r["error"].get("message")}

def short(a): return a[:10] if isinstance(a, str) else a

def snapshot(port, num):
    h = hex(num)
    ci = rpc(port, "kaia_getBlockWithConsensusInfoByNumber", [h]) or {}
    blk = rpc(port, "kaia_getBlockByNumber", [h, False]) or {}
    if "error" in ci or "error" in blk or not blk:
        return None
    vals = rpc(port, "istanbul_getValidators", [h])
    nodes = rpc(port, "kaia_getNodesByState", [h, []])
    pfs = rpc(port, "governance_getPFS", [h])
    states = None  # None = not answerable on this node (e.g. state pruned on a full node); excluded from the verdict
    if isinstance(nodes, dict) and "error" not in nodes:
        states = {}
        for addr, n in nodes.items():
            states[short(addr)] = n.get("state") if isinstance(n, dict) else n
    return {
        "hash": blk.get("hash"), "stateRoot": blk.get("stateRoot"),
        "round": int(ci.get("round", 0), 16) if isinstance(ci.get("round"), str) else ci.get("round"),
        "proposer": short(ci.get("proposer")), "originProposer": short(ci.get("originProposer")),
        "committers": sorted(short(c) for c in (ci.get("committers") or [])),
        "qualified": sorted(short(v) for v in vals) if isinstance(vals, list) else vals,
        "nodeStates": dict(sorted(states.items())) if states is not None else None,
        "pfs": {short(k): v for k, v in sorted(pfs.items())} if isinstance(pfs, dict) and "error" not in pfs else pfs,
    }

def main():
    if len(sys.argv) < 2:
        print(__doc__); sys.exit(2)
    H = int(sys.argv[1]); nodes = dict(CN); 
    if "--ens" in sys.argv: nodes.update(EN)
    for num in (H, H + 1, H + 2):
        print(f"=== block {num} ({'H' if num == H else 'H+%d' % (num - H)}) ===")
        snaps = {}
        for name, port in nodes.items():
            s = snapshot(port, num)
            if s is None:
                print(f"  {name}: not available"); continue
            snaps[name] = s
            print(f"  {name}: round={s['round']} proposer={s['proposer']} origin={s['originProposer']} committers={s['committers']}")
            print(f"       hash={short(s['hash'])} stateRoot={short(s['stateRoot'])} qualified={s['qualified']}")
            print(f"       states={s['nodeStates']} pfs={s['pfs']}")
        for field in ("hash", "stateRoot", "qualified", "nodeStates", "pfs", "proposer", "originProposer", "round", "committers"):
            na = [n for n, s in snaps.items() if s[field] is None or (isinstance(s[field], dict) and "error" in s[field])]
            vals = {n: json.dumps(s[field], sort_keys=True) for n, s in snaps.items() if n not in na}
            distinct = set(vals.values())
            if na:
                print(f"  [{field:14s}] n/a on {na} (state not available there; compared on the rest)")
            if not vals:
                continue
            if field == "committers":
                # committed seals live outside the block hash: each node seals with the quorum it saw first,
                # so the subset may differ per node at any height. Informational only; at H the holder's
                # set is the round-0 quorum and the others' the round-1 quorum.
                tag = "same" if len(distinct) == 1 else "varies by node (seal subset, informational)"
                print(f"  [{field:14s}] {tag}" + ("" if len(distinct) == 1 else "  " + "; ".join(f"{n}={v[:60]}" for n, v in vals.items())))
                continue
            expect_split = num == H and field == "round"
            tag = "same" if len(distinct) == 1 else ("DIFFERS (expected at H)" if expect_split else "DIFFERS")
            print(f"  [{field:14s}] {tag}" + ("" if len(distinct) == 1 else "  " + "; ".join(f"{n}={v[:60]}" for n, v in vals.items())))

if __name__ == "__main__":
    main()
