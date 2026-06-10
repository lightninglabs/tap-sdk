package rest

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/tap-sdk/macaroon"
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
