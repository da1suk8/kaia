// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

contract P256VerifyWrapper {
    function verify(bytes calldata input) external view returns (bool ok, bytes memory ret) {
        bytes memory out;
        bool success;
        assembly {
            let dataLen := input.length
            let dataPtr := mload(0x40)
            calldatacopy(dataPtr, input.offset, dataLen)

            success := staticcall(gas(), 0x100, dataPtr, dataLen, 0, 0)

            let size := returndatasize()
            out := mload(0x40)
            mstore(out, size)
            mstore(0x40, add(out, add(size, 0x20)))
            returndatacopy(add(out, 0x20), 0, size)
        }
        ok = success && out.length == 32 && out[31] == 0x01;
        ret = out;
    }
}