//go:build itest

package itest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/stretchr/testify/require"
)

// MineBlocks mines the specified number of blocks via bitcoind using the miner
// wallet.
func (h *TestHarness) MineBlocks(n int) {
	h.t.Helper()

	h.ensureMinerWallet()

	addr := h.bitcoindRPCWallet(
		"miner", "getnewaddress", `""`, `"bech32"`,
	)
	addr = strings.TrimSpace(strings.Trim(addr, `"`))

	result := h.bitcoindRPCWallet(
		"miner", "generatetoaddress",
		fmt.Sprintf("%d", n),
		fmt.Sprintf(`"%s"`, addr),
	)
	h.t.Logf("Mined %d blocks: %s", n, truncate(result, 120))
}

// ensureMinerWallet creates the regtest miner wallet once if needed.
func (h *TestHarness) ensureMinerWallet() {
	h.t.Helper()

	wallets := h.bitcoindRPC("listwallets")
	if strings.Contains(wallets, `"miner"`) {
		return
	}

	h.bitcoindRPC("createwallet", `"miner"`)
}

// FundLndWallet sends BTC from bitcoind to Alice's LND wallet and waits for
// both tapd nodes to catch up.
func (h *TestHarness) FundLndWallet() {
	h.t.Helper()

	h.MineBlocks(110)

	aliceAddr := h.lndNewAddress("tap-sdk-lnd-alice")
	h.t.Logf("Alice LND address: %s", aliceAddr)

	h.bitcoindRPCWallet("miner", "sendtoaddress",
		fmt.Sprintf(`"%s"`, aliceAddr), `1.0`)

	h.MineBlocks(defaultMineBlocks)

	h.t.Logf("Funded Alice LND wallet and mined %d confirms",
		defaultMineBlocks)

	h.WaitForSync(h.AliceClient, 60*time.Second)
	h.WaitForSync(h.BobClient, 60*time.Second)
}

// lndNewAddress generates a new p2wkh address from an LND container using
// lncli.
func (h *TestHarness) lndNewAddress(container string) string {
	h.t.Helper()

	out, err := exec.Command(
		"docker", "exec", container,
		"lncli", "--network=regtest", "newaddress", "p2wkh",
	).CombinedOutput()
	require.NoError(h.t, err,
		"lncli newaddress failed: %s", string(out))

	var resp struct {
		Address string `json:"address"`
	}
	require.NoError(h.t, json.Unmarshal(out, &resp),
		"failed to parse lncli output: %s", string(out))

	return resp.Address
}

// bitcoindRPCWallet calls bitcoind JSON-RPC targeting a specific named wallet.
func (h *TestHarness) bitcoindRPCWallet(wallet, method string,
	params ...string) string {

	h.t.Helper()

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
	require.NoError(h.t, err,
		"bitcoind RPC (%s, wallet=%s) failed: %s",
		method, wallet, string(out))

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(out, &rpcResp); err == nil {
		if string(rpcResp.Error) != "null" &&
			string(rpcResp.Error) != "" {

			h.t.Logf("bitcoind RPC error: %s", string(rpcResp.Error))
		}
		return string(rpcResp.Result)
	}

	return string(out)
}

// bitcoindRPC executes a bitcoin-cli RPC command against the regtest bitcoind.
// If bitcoin-cli is unavailable, it falls back to curl-based JSON-RPC.
func (h *TestHarness) bitcoindRPC(method string,
	params ...string) string {

	h.t.Helper()

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
		return h.bitcoindCurlRPC(method, params...)
	}

	return stdout.String()
}

// bitcoindCurlRPC performs a JSON-RPC call to bitcoind via curl.
func (h *TestHarness) bitcoindCurlRPC(method string,
	params ...string) string {

	h.t.Helper()

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
	require.NoError(h.t, err)

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
	require.NoError(h.t, err,
		"bitcoind RPC %s failed: %s", method, stderr.String())

	type rpcResponse struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	var resp rpcResponse
	err = json.Unmarshal(stdout.Bytes(), &resp)
	require.NoError(h.t, err)

	if string(resp.Error) != "null" && string(resp.Error) != "" {
		h.t.Fatalf("bitcoind RPC %s error: %s", method,
			string(resp.Error))
	}

	return string(resp.Result)
}
