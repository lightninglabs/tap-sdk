package rest

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/macaroon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomAnchorCapabilities(t *testing.T) {
	client := newWalletKitClient(&transport{
		baseURL:   "http://example.invalid",
		client:    http.DefaultClient,
		timeout:   time.Second,
		macaroons: macaroon.Pouch{},
	})

	caps, err := client.CustomAnchorCapabilities(context.Background())
	require.NoError(t, err)
	require.Equal(t, tapsdk.DefaultTapdCustomAnchorCapabilities(), *caps)
}

func TestVerifyAssetOwnershipSendsChallenge(t *testing.T) {
	proof := []byte("proof")
	challenge := make([]byte, 32)
	var txid [32]byte
	for i := range challenge {
		challenge[i] = byte(i + 1)
		txid[i] = byte(i + 2)
	}
	responseBody := fmt.Sprintf(
		`{"valid_proof":true,"outpoint":{"txid":%q,"output_index":1}}`,
		hex.EncodeToString(txid[:]),
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,
		r *http.Request) {

		if r.URL.Path != "/v1/taproot-assets/wallet/ownership/verify" {
			t.Errorf("unexpected path: got %s", r.URL.Path)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
			return
		}

		if body["proof_with_witness"] != hex.EncodeToString(proof) {
			t.Errorf("unexpected proof: got %s",
				body["proof_with_witness"])
		}
		if body["challenge"] != hex.EncodeToString(challenge) {
			t.Errorf("unexpected challenge: got %s", body["challenge"])
		}

		_, err := fmt.Fprint(w, responseBody)
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()

	client := newWalletKitClient(&transport{
		baseURL:   srv.URL,
		client:    srv.Client(),
		timeout:   time.Second,
		macaroons: macaroon.Pouch{},
	})

	resp, err := client.VerifyAssetOwnership(
		context.Background(),
		&tapsdk.VerifyOwnershipRequest{
			ProofWithWitness: proof,
			Challenge:        challenge,
		},
	)
	require.NoError(t, err)
	require.True(t, resp.Valid)
}

func TestProveAssetOwnershipPreservesRequestMetadata(t *testing.T) {
	proof := []byte("proof")
	var assetID tapsdk.AssetID
	var scriptKey tapsdk.PubKey
	var outpoint tapsdk.Outpoint
	for i := range assetID {
		assetID[i] = byte(i + 1)
		scriptKey[i] = byte(i + 2)
		outpoint.Txid[i] = byte(i + 3)
	}
	scriptKey[32] = 99
	outpoint.Index = 7
	ref := tapsdk.AssetRefFromAssetID(assetID)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,
		r *http.Request) {

		if r.URL.Path != "/v1/taproot-assets/wallet/ownership/prove" {
			t.Errorf("unexpected path: got %s", r.URL.Path)
		}

		var body struct {
			AssetID   string `json:"asset_id"`
			ScriptKey string `json:"script_key"`
			Outpoint  struct {
				Txid        string `json:"txid"`
				OutputIndex uint32 `json:"output_index"`
			} `json:"outpoint"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
			return
		}

		if body.AssetID != hex.EncodeToString(assetID[:]) {
			t.Errorf("unexpected asset ID: got %s", body.AssetID)
		}
		if body.ScriptKey != hex.EncodeToString(scriptKey[:]) {
			t.Errorf("unexpected script key: got %s", body.ScriptKey)
		}
		if body.Outpoint.Txid != hex.EncodeToString(outpoint.Txid[:]) {
			t.Errorf("unexpected outpoint txid: got %s",
				body.Outpoint.Txid)
		}
		if body.Outpoint.OutputIndex != outpoint.Index {
			t.Errorf("unexpected output index: got %d",
				body.Outpoint.OutputIndex)
		}

		_, err := fmt.Fprintf(
			w, `{"proof_with_witness":%q}`,
			hex.EncodeToString(proof),
		)
		if err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()

	client := newWalletKitClient(&transport{
		baseURL:   srv.URL,
		client:    srv.Client(),
		timeout:   time.Second,
		macaroons: macaroon.Pouch{},
	})

	resp, err := client.ProveAssetOwnership(
		context.Background(),
		&tapsdk.ProveOwnershipRequest{
			AssetRef:  ref,
			ScriptKey: scriptKey,
			Outpoint:  outpoint,
		},
	)
	require.NoError(t, err)
	require.Equal(t, ref, resp.AssetRef)
	require.Equal(t, assetID, resp.IssuanceID)
	require.Equal(t, scriptKey, resp.ScriptKey)
	require.Equal(t, outpoint, resp.Outpoint)
	require.Equal(t, proof, resp.ProofWithWitness)
}

func TestCommitVirtualPsbtsSendsAdvancedFields(t *testing.T) {
	feeRate, err := tapsdk.NewFeeRateSatPerVByte(11)
	require.NoError(t, err)

	var locked tapsdk.Outpoint
	for i := range locked.Txid {
		locked.Txid[i] = byte(i + 1)
	}
	locked.Index = 7

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,
		r *http.Request) {

		assert.Equal(
			t, "/v1/taproot-assets/wallet/virtual-psbt/commit",
			r.URL.Path,
		)

		var body struct {
			AnchorPsbt            string   `json:"anchor_psbt"`
			VirtualPsbts          []string `json:"virtual_psbts"`
			PassiveAssetPsbts     []string `json:"passive_asset_psbts"`
			ExistingOutputIndex   *int32   `json:"existing_output_index"`
			Add                   *bool    `json:"add"`
			SatPerVByte           string   `json:"sat_per_vbyte"`
			CustomLockID          string   `json:"custom_lock_id"`
			LockExpirationSeconds string   `json:"lock_expiration_seconds"`
		}
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			return
		}
		assert.Equal(t, hex.EncodeToString([]byte("anchor")),
			body.AnchorPsbt)
		assert.Equal(t, []string{hex.EncodeToString([]byte("virtual"))},
			body.VirtualPsbts)
		assert.Equal(t, []string{hex.EncodeToString([]byte("passive"))},
			body.PassiveAssetPsbts)
		if !assert.NotNil(t, body.ExistingOutputIndex) {
			return
		}
		assert.Equal(t, int32(0), *body.ExistingOutputIndex)
		assert.Nil(t, body.Add)
		assert.Equal(t, "11", body.SatPerVByte)
		assert.Equal(t, hex.EncodeToString([]byte("lock")),
			body.CustomLockID)
		assert.Equal(t, "42", body.LockExpirationSeconds)

		_, err := fmt.Fprintf(w, `{
			"anchor_psbt":%q,
			"virtual_psbts":[%q],
			"passive_asset_psbts":[%q],
			"change_output_index":2,
			"lnd_locked_utxos":[{
				"txid":%q,
				"output_index":7
			}]
		}`,
			hex.EncodeToString([]byte("committed-anchor")),
			hex.EncodeToString([]byte("committed-virtual")),
			hex.EncodeToString([]byte("committed-passive")),
			hex.EncodeToString(locked.Txid[:]),
		)
		assert.NoError(t, err)
	}))
	defer srv.Close()

	client := newWalletKitClient(&transport{
		baseURL:   srv.URL,
		client:    srv.Client(),
		timeout:   time.Second,
		macaroons: macaroon.Pouch{},
	})

	resp, err := client.CommitVirtualPsbts(
		context.Background(), &tapsdk.CommitVirtualPsbtsRequest{
			AnchorPsbt:        []byte("anchor"),
			VirtualPsbts:      [][]byte{[]byte("virtual")},
			PassiveAssetPsbts: [][]byte{[]byte("passive")},
			Funding: tapsdk.AnchorFundingPlan{
				ChangeOutput: tapsdk.AnchorChangeOutput{
					Mode: tapsdk.AnchorChangeOutputExisting,
				},
				Fee: tapsdk.AnchorFee{
					Mode:    tapsdk.AnchorFeeSatPerVByte,
					FeeRate: feeRate,
				},
				CustomLockID:          []byte("lock"),
				LockExpirationSeconds: 42,
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, []byte("committed-anchor"), resp.AnchorPsbt)
	require.Equal(t, [][]byte{[]byte("committed-virtual")},
		resp.VirtualPsbts)
	require.Equal(t, [][]byte{[]byte("committed-passive")},
		resp.PassiveAssetPsbts)
	require.Equal(t, int32(2), resp.ChangeOutputIndex)
	require.Equal(t, []tapsdk.Outpoint{locked}, resp.LockedUTXOs)
}

func TestPublishAndLogTransferSendsAdvancedFields(t *testing.T) {
	var locked tapsdk.Outpoint
	for i := range locked.Txid {
		locked.Txid[i] = byte(i + 2)
	}
	locked.Index = 9

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,
		r *http.Request) {

		assert.Equal(
			t, "/v1/taproot-assets/wallet/virtual-psbt/log-transfer",
			r.URL.Path,
		)

		bodyBytes, err := io.ReadAll(r.Body)
		if !assert.NoError(t, err) {
			return
		}

		var raw map[string]json.RawMessage
		if !assert.NoError(t, json.Unmarshal(bodyBytes, &raw)) {
			return
		}
		_, ok := raw["change_output_index"]
		assert.True(t, ok)

		var body struct {
			AnchorPsbt        string   `json:"anchor_psbt"`
			VirtualPsbts      []string `json:"virtual_psbts"`
			PassiveAssetPsbts []string `json:"passive_asset_psbts"`
			ChangeOutputIndex int32    `json:"change_output_index"`
			LndLockedUtxos    []struct {
				Txid        string `json:"txid"`
				OutputIndex uint32 `json:"output_index"`
			} `json:"lnd_locked_utxos"`
			SkipAnchorTxBroadcast bool   `json:"skip_anchor_tx_broadcast"`
			Label                 string `json:"label"`
		}
		if !assert.NoError(t, json.Unmarshal(bodyBytes, &body)) {
			return
		}
		assert.Equal(t, hex.EncodeToString([]byte("anchor")),
			body.AnchorPsbt)
		assert.Equal(t, []string{hex.EncodeToString([]byte("virtual"))},
			body.VirtualPsbts)
		assert.Equal(t, []string{hex.EncodeToString([]byte("passive"))},
			body.PassiveAssetPsbts)
		assert.Equal(t, int32(0), body.ChangeOutputIndex)
		if !assert.Len(t, body.LndLockedUtxos, 1) {
			return
		}
		assert.Equal(t, hex.EncodeToString(locked.Txid[:]),
			body.LndLockedUtxos[0].Txid)
		assert.Equal(t, uint32(9),
			body.LndLockedUtxos[0].OutputIndex)
		assert.True(t, body.SkipAnchorTxBroadcast)
		assert.Equal(t, "custom-label", body.Label)

		_, err = fmt.Fprintf(
			w, `{"transfer":{"anchor_tx":%q}}`,
			hex.EncodeToString([]byte("final-anchor")),
		)
		assert.NoError(t, err)
	}))
	defer srv.Close()

	client := newWalletKitClient(&transport{
		baseURL:   srv.URL,
		client:    srv.Client(),
		timeout:   time.Second,
		macaroons: macaroon.Pouch{},
	})

	resp, err := client.PublishAndLogTransfer(
		context.Background(), &tapsdk.PublishAndLogTransferRequest{
			AnchorPsbt:            []byte("anchor"),
			VirtualPsbts:          [][]byte{[]byte("virtual")},
			PassiveAssetPsbts:     [][]byte{[]byte("passive")},
			ChangeOutputIndex:     0,
			LockedUTXOs:           []tapsdk.Outpoint{locked},
			SkipAnchorTxBroadcast: true,
			Label:                 "custom-label",
		},
	)
	require.NoError(t, err)
	require.Equal(t, []byte("final-anchor"), resp.AnchorTransaction)
	require.Equal(t, [][]byte{[]byte("virtual")},
		resp.VirtualTransactions)
	require.Equal(t, [][]byte{[]byte("passive")},
		resp.PassiveAssetTransactions)
}

func TestVerifyAssetOwnershipRejectsInvalidChallenge(t *testing.T) {
	client := newWalletKitClient(&transport{
		baseURL:   "http://example.invalid",
		client:    http.DefaultClient,
		timeout:   time.Second,
		macaroons: macaroon.Pouch{},
	})

	_, err := client.VerifyAssetOwnership(
		context.Background(),
		&tapsdk.VerifyOwnershipRequest{
			ProofWithWitness: []byte("proof"),
			Challenge:        make([]byte, 32),
		},
	)
	require.ErrorContains(t, err, "all zero")
}
