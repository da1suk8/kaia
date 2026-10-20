// vrankdump decodes header.VRank of one block and recovers the committers of
// the parent-round certificate it carries.
//
//	go run . <rpc-url> <block-number>
//
// Output is one JSON object: the block number and hash, the parent hash, the
// decoded Report, ParentRound, the number of parent committed seals and the
// addresses recovered from them over (parentHash, ParentRound).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/kaiachain/kaia/common"
	"github.com/kaiachain/kaia/common/hexutil"
	"github.com/kaiachain/kaia/consensus/istanbul"
	"github.com/kaiachain/kaia/kaiax/vrank"
)

type rpcHeader struct {
	Number     string        `json:"number"`
	Hash       common.Hash   `json:"hash"`
	ParentHash common.Hash   `json:"parentHash"`
	VRank      hexutil.Bytes `json:"vrank"`
}

type out struct {
	Number           uint64           `json:"number"`
	Hash             common.Hash      `json:"hash"`
	ParentHash       common.Hash      `json:"parentHash"`
	VRankHex         string           `json:"vrank"`
	Report           []common.Address `json:"report"`
	ParentRound      uint8            `json:"parentRound"`
	ParentSeals      int              `json:"parentSeals"`
	ParentCommitters []common.Address `json:"parentCommitters"`
	Error            string           `json:"error,omitempty"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: vrankdump <rpc-url> <block-number>")
		os.Exit(2)
	}
	num, err := strconv.ParseUint(os.Args[2], 10, 64)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad block number:", err)
		os.Exit(2)
	}
	var h rpcHeader
	if err := call(os.Args[1], "kaia_getHeaderByNumber", []any{hexutil.EncodeUint64(num)}, &h); err != nil {
		fmt.Fprintln(os.Stderr, "rpc:", err)
		os.Exit(1)
	}
	o := out{Number: num, Hash: h.Hash, ParentHash: h.ParentHash, VRankHex: h.VRank.String()}
	payload, err := vrank.DecodeVRank(h.VRank)
	if err != nil {
		o.Error = "decode: " + err.Error()
	} else {
		o.Report, o.ParentRound, o.ParentSeals = payload.Report, payload.ParentRound, len(payload.ParentCommittedSeal)
		preimage := istanbul.PrepareCommittedSealWithRound(h.ParentHash, payload.ParentRound)
		for _, seal := range payload.ParentCommittedSeal {
			addr, err := istanbul.GetSignatureAddress(preimage, seal)
			if err != nil {
				o.Error = "seal: " + err.Error()
				break
			}
			o.ParentCommitters = append(o.ParentCommitters, addr)
		}
	}
	enc, _ := json.MarshalIndent(o, "", "  ")
	fmt.Println(string(enc))
}

func call(url, method string, params []any, result any) error {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	if env.Error != nil {
		return fmt.Errorf("%s: %s", method, env.Error.Message)
	}
	if string(env.Result) == "null" {
		return fmt.Errorf("%s: no such block", method)
	}
	return json.Unmarshal(env.Result, result)
}
