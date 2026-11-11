//go:build itest

package itest

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/stretchr/testify/require"
)

// MineBlocks mines the specified number of blocks via bitcoind using the miner
// wallet.
func (h *TestHarness) MineBlocks(t testing.TB, n int) {
	t.Helper()

	h.ensureMinerWallet(t)

	addr := h.bitcoindRPCWallet(
		t,
		"miner", "getnewaddress", `""`, `"bech32"`,
	)
	addr = strings.TrimSpace(strings.Trim(addr, `"`))

	h.bitcoindRPCWallet(
		t,
		"miner", "generatetoaddress",
		fmt.Sprintf("%d", n),
		fmt.Sprintf(`"%s"`, addr),
	)
}

// ensureMinerWallet creates the regtest miner wallet once if needed.
func (h *TestHarness) ensureMinerWallet(t testing.TB) {
	t.Helper()

	wallets := h.bitcoindRPC(t, "listwallets")
	if strings.Contains(wallets, `"miner"`) {
		return
	}

	h.bitcoindRPC(t, "createwallet", `"miner"`)
}

// FundLndWallet sends BTC from bitcoind to Alice's LND wallet and waits for
// both tapd nodes to catch up.
func (h *TestHarness) FundLndWallet(t testing.TB, ctx context.Context) {
	t.Helper()

	h.MineBlocks(t, 110)

	aliceAddr := h.lndNewAddress(t, "tap-sdk-lnd-alice")

	h.bitcoindRPCWallet(t, "miner", "sendtoaddress",
		fmt.Sprintf(`"%s"`, aliceAddr), `1.0`)

	h.MineBlocks(t, defaultMineBlocks)

	h.WaitForSync(t, ctx, h.AliceClient, 60*time.Second)
	h.WaitForSync(t, ctx, h.BobClient, 60*time.Second)
}

// fundedOutput is a confirmed transaction output.
type fundedOutput struct {
	outpoint wire.OutPoint
	txOut    *wire.TxOut
}

// fundScript pays a confirmed output to pkScript from the bitcoind miner
// wallet.
func (h *TestHarness) fundScript(t testing.TB, ctx context.Context,
	pkScript []byte) fundedOutput {

	t.Helper()

	decodedJSON := h.bitcoindRPCWallet(
		t, "miner", "decodescript",
		fmt.Sprintf("%q", hex.EncodeToString(pkScript)),
	)
	var decoded struct {
		Address string `json:"address"`
	}
	require.NoError(t, json.Unmarshal([]byte(decodedJSON), &decoded))
	require.NotEmpty(t, decoded.Address)

	txidJSON := h.bitcoindRPCWallet(
		t, "miner", "sendtoaddress",
		fmt.Sprintf("%q", decoded.Address), "0.001",
	)
	var txid string
	require.NoError(t, json.Unmarshal([]byte(txidJSON), &txid))
	txJSON := h.bitcoindRPCWallet(
		t, "miner", "gettransaction", fmt.Sprintf("%q", txid),
		"true", "true",
	)
	var funding struct {
		Decoded struct {
			Vout []struct {
				Value        float64 `json:"value"`
				N            uint32  `json:"n"`
				ScriptPubKey struct {
					Hex string `json:"hex"`
				} `json:"scriptPubKey"`
			} `json:"vout"`
		} `json:"decoded"`
	}
	require.NoError(t, json.Unmarshal([]byte(txJSON), &funding))

	hash, err := chainhash.NewHashFromStr(txid)
	require.NoError(t, err)
	for _, vout := range funding.Decoded.Vout {
		if vout.ScriptPubKey.Hex != hex.EncodeToString(pkScript) {
			continue
		}
		amount, err := btcutil.NewAmount(vout.Value)
		require.NoError(t, err)

		h.MineBlocks(t, 1)
		h.WaitForSync(t, ctx, h.AliceClient, defaultSyncTimeout)

		return fundedOutput{
			outpoint: wire.OutPoint{Hash: *hash, Index: vout.N},
			txOut: &wire.TxOut{
				Value:    int64(amount),
				PkScript: pkScript,
			},
		}
	}

	require.FailNow(t, "funding transaction has no output for the script")
	return fundedOutput{}
}

// lndNewAddress generates a new p2wkh address from an LND container using
// lncli.
func (h *TestHarness) lndNewAddress(t testing.TB, container string) string {
	t.Helper()

	out, err := exec.Command(
		"docker", "exec", container,
		"lncli", "--network=regtest", "newaddress", "p2wkh",
	).CombinedOutput()
	require.NoError(t, err,
		"lncli newaddress failed: %s", string(out))

	var resp struct {
		Address string `json:"address"`
	}
	require.NoError(t, json.Unmarshal(out, &resp),
		"failed to parse lncli output: %s", string(out))

	return resp.Address
}

// bitcoindRPCWallet calls bitcoind JSON-RPC targeting a specific named wallet.
func (h *TestHarness) bitcoindRPCWallet(t testing.TB,
	wallet, method string,
	params ...string) string {
	t.Helper()

	paramStr := strings.Join(params, ", ")
	body := fmt.Sprintf(
		`{"jsonrpc":"1.0","id":"test","method":"%s","params":[%s]}`,
		method, paramStr,
	)

	url := fmt.Sprintf(
		"http://%s:%s@%s/wallet/%s",
		bitcoindUser, bitcoindPass, h.bitcoindHost, wallet,
	)

	out, err := exec.Command(
		"curl", "-s", "-X", "POST",
		"-H", "Content-Type: application/json",
		"-d", body,
		url,
	).CombinedOutput()
	require.NoError(t, err,
		"bitcoind RPC (%s, wallet=%s) failed: %s",
		method, wallet, string(out))

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(out, &rpcResp); err == nil {
		if string(rpcResp.Error) != "null" &&
			string(rpcResp.Error) != "" {

			t.Logf("bitcoind RPC error: %s", string(rpcResp.Error))
		}
		return string(rpcResp.Result)
	}

	return string(out)
}

// bitcoindRPC executes a bitcoin-cli RPC command against the regtest bitcoind.
// If bitcoin-cli is unavailable, it falls back to curl-based JSON-RPC.
func (h *TestHarness) bitcoindRPC(t testing.TB, method string,
	params ...string) string {
	t.Helper()

	args := []string{
		"-regtest",
		fmt.Sprintf("-rpcconnect=%s", strings.Split(h.bitcoindHost, ":")[0]),
		fmt.Sprintf("-rpcport=%s", strings.Split(h.bitcoindHost, ":")[1]),
		fmt.Sprintf("-rpcuser=%s", bitcoindUser),
		fmt.Sprintf("-rpcpassword=%s", bitcoindPass),
		method,
	}
	args = append(args, params...)

	cmd := exec.Command("bitcoin-cli", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return h.bitcoindCurlRPC(t, method, params...)
	}

	return stdout.String()
}

// bitcoindCurlRPC performs a JSON-RPC call to bitcoind via curl.
func (h *TestHarness) bitcoindCurlRPC(t testing.TB, method string,
	params ...string) string {
	t.Helper()

	jsonParams := make([]json.RawMessage, 0, len(params))
	for _, p := range params {
		jsonParams = append(jsonParams, json.RawMessage(p))
	}

	type rpcRequest struct {
		JSONRPC string            `json:"jsonrpc"`
		ID      string            `json:"id"`
		Method  string            `json:"method"`
		Params  []json.RawMessage `json:"params"`
	}

	reqBody, err := json.Marshal(rpcRequest{
		JSONRPC: "1.0",
		ID:      "itest",
		Method:  method,
		Params:  jsonParams,
	})
	require.NoError(t, err)

	url := fmt.Sprintf("http://%s:%s@%s/",
		bitcoindUser, bitcoindPass, h.bitcoindHost)

	cmd := exec.Command("curl", "-s", "-X", "POST",
		"-H", "Content-Type: application/json",
		"-d", string(reqBody), url,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	require.NoError(t, err,
		"bitcoind RPC %s failed: %s", method, stderr.String())

	type rpcResponse struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	var resp rpcResponse
	err = json.Unmarshal(stdout.Bytes(), &resp)
	require.NoError(t, err)

	if string(resp.Error) != "null" && string(resp.Error) != "" {
		t.Fatalf("bitcoind RPC %s error: %s", method,
			string(resp.Error))
	}

	return string(resp.Result)
}
