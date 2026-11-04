#!/usr/bin/env python3
"""states.py <rpc-port> <height> [<height> ...]: node states (kaia_getNodesByState) per height, one line each."""
import json, sys, urllib.request
port = int(sys.argv[1])
def call(m, p):
    req = urllib.request.Request(f'http://127.0.0.1:{port}', json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': m, 'params': p}).encode(), {'Content-Type': 'application/json'})
    return json.load(urllib.request.urlopen(req, timeout=5))
for h in sys.argv[2:]:
    n = int(h, 0) if h != 'latest' else h
    r = call('kaia_getNodesByState', [hex(n) if n != 'latest' else n, []])
    if 'error' in r:
        print(n, 'error', r['error'].get('message')); continue
    d = r['result']
    print(n, ' '.join(f"{k[:6]}={v.get('state')}" for k, v in sorted(d.items())))
