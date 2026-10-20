#!/usr/bin/env python3
"""Fill in the kip71 defaults homi leaves at zero.

With --governance, homi writes a kip71 block with every field set to 0. A
baseFeeDenominator of 0 is rejected outright, so `kcn init` panics with
"failed to add param kip71.basefeedenominator: invalid param value" before it
ever writes a chaindata directory. The values below are the client's own
defaults from params/governance_params.go.
"""
import json
import sys

DEFAULTS = {
    'lowerboundbasefee': 25000000000,
    'upperboundbasefee': 750000000000,
    'gastarget': 30000000,
    'maxblockgasusedforbasefee': 60000000,
    'basefeedenominator': 20,
}


def main(path):
    with open(path) as f:
        genesis = json.load(f)
    kip71 = genesis.get('config', {}).get('governance', {}).get('kip71')
    if kip71 is None:
        print('no kip71 block; nothing to patch')
        return 0
    if kip71.get('basefeedenominator'):
        print('kip71 already populated; nothing to patch')
        return 0
    kip71.update(DEFAULTS)
    with open(path, 'w') as f:
        json.dump(genesis, f, indent=4)
    print('patched kip71 defaults into', path)
    return 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1]))
