package rest

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/tap-sdk/macaroon"
)

// walletKitClient implements tapsdk.WalletKitClient over REST.
type walletKitClient struct {
	transport *transport
}

func newWalletKitClient(tp *transport) *walletKitClient {
	return &walletKitClient{transport: tp}
}

// jsonNextScriptKeyRequest is the JSON body for NextScriptKey.
type jsonNextScriptKeyRequest struct {
	KeyFamily uint32 `json:"key_family"`
}

// DeriveScriptKey derives a new script key for receiving assets.
func (w *walletKitClient) DeriveScriptKey(
	ctx context.Context) (*entities.ScriptKey, error) {

	body := &jsonNextScriptKeyRequest{
		KeyFamily: entities.TaprootAssetsKeyFamily,
	}

	var resp jsonNextScriptKeyResponse
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/wallet/script-key/next",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	if resp.ScriptKey == nil {
		return nil, fmt.Errorf("invalid script key response")
	}

	return unmarshalScriptKey(resp.ScriptKey)
}

// jsonNextInternalKeyRequest is the JSON body for
// NextInternalKey.
type jsonNextInternalKeyRequest struct {
	KeyFamily uint32 `json:"key_family"`
}

// DeriveInternalKey derives a new internal key for anchor outputs.
func (w *walletKitClient) DeriveInternalKey(
	ctx context.Context) (*entities.InternalKey, error) {

	body := &jsonNextInternalKeyRequest{
		KeyFamily: entities.TaprootAssetsKeyFamily,
	}

	var resp jsonNextInternalKeyResponse
	err := w.transport.doPost(
		ctx,
		"/v1/taproot-assets/wallet/internal-key/next",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	if resp.InternalKey == nil {
		return nil, fmt.Errorf("invalid internal key response")
	}

	keyDesc, err := unmarshalKeyDescriptor(resp.InternalKey)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to unmarshal internal key: %w", err,
		)
	}

	return &entities.InternalKey{
		PubKey:     keyDesc.RawKeyBytes,
		KeyLocator: keyDesc.KeyLocator,
	}, nil
}

// jsonFundVirtualPsbtRequest is the JSON body for
// FundVirtualPsbt (raw template variant).
type jsonFundVirtualPsbtRequest struct {
	Raw  *jsonTxTemplate `json:"raw,omitempty"`
	Psbt string          `json:"psbt,omitempty"`
}

// jsonTxTemplate is the JSON shape of
// assetwalletrpc.TxTemplate.
type jsonTxTemplate struct {
	Inputs []*jsonPrevIDReq `json:"inputs,omitempty"`

	AddressesWithAmounts []*jsonAddressWithAmount `json:"addresses_with_amounts,omitempty"` //nolint:lll
}

// jsonPrevIDReq is the JSON shape of assetwalletrpc.PrevId.
type jsonPrevIDReq struct {
	Outpoint  *jsonOutpointReq `json:"outpoint"`
	ID        string           `json:"id"`
	ScriptKey string           `json:"script_key"`
}

// FundTransfer funds a virtual transaction using addresses.
func (w *walletKitClient) FundTransfer(ctx context.Context,
	recipients []entities.Recipient,
	inputs []entities.PrevID) (*entities.FundedTransfer, error) {

	rpcInputs := make([]*jsonPrevIDReq, 0, len(inputs))
	for _, input := range inputs {
		rpcInputs = append(rpcInputs, &jsonPrevIDReq{
			Outpoint: &jsonOutpointReq{
				Txid: base64.StdEncoding.EncodeToString(
					input.Outpoint.Txid[:],
				),
				OutputIndex: input.Outpoint.Index,
			},
			ID: base64.StdEncoding.EncodeToString(
				input.AssetID[:],
			),
			ScriptKey: base64.StdEncoding.EncodeToString(
				input.ScriptKey[:],
			),
		})
	}

	rpcRecipients := make(
		[]*jsonAddressWithAmount, 0, len(recipients),
	)
	for _, r := range recipients {
		rpcRecipients = append(rpcRecipients,
			&jsonAddressWithAmount{
				TapAddr: r.Address,
				Amount: fmt.Sprintf(
					"%d", r.Amount,
				),
			},
		)
	}

	body := &jsonFundVirtualPsbtRequest{
		Raw: &jsonTxTemplate{
			Inputs:               rpcInputs,
			AddressesWithAmounts: rpcRecipients,
		},
	}

	var resp jsonFundVirtualPsbtResponse
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/wallet/virtual-psbt/fund",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	fundedPsbt, err := parseBase64Bytes(resp.FundedPsbt)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid funded_psbt: %w", err,
		)
	}

	passivePsbts := make([][]byte, 0, len(resp.PassiveAssetPsbts))
	for _, p := range resp.PassiveAssetPsbts {
		psbt, err := parseBase64Bytes(p)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid passive_asset_psbt: %w", err,
			)
		}

		passivePsbts = append(passivePsbts, psbt)
	}

	return &entities.FundedTransfer{
		FundedPsbt:        fundedPsbt,
		PassiveAssetPsbts: passivePsbts,
	}, nil
}

// FundInteractivePsbt funds a virtual PSBT for interactive sends.
func (w *walletKitClient) FundInteractivePsbt(ctx context.Context,
	psbt []byte) (*entities.FundedTransfer, error) {

	body := &jsonFundVirtualPsbtRequest{
		Psbt: base64.StdEncoding.EncodeToString(psbt),
	}

	var resp jsonFundVirtualPsbtResponse
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/wallet/virtual-psbt/fund",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	fundedPsbt, err := parseBase64Bytes(resp.FundedPsbt)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid funded_psbt: %w", err,
		)
	}

	passivePsbts := make([][]byte, 0, len(resp.PassiveAssetPsbts))
	for _, p := range resp.PassiveAssetPsbts {
		psbt, err := parseBase64Bytes(p)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid passive_asset_psbt: %w", err,
			)
		}

		passivePsbts = append(passivePsbts, psbt)
	}

	return &entities.FundedTransfer{
		FundedPsbt:        fundedPsbt,
		PassiveAssetPsbts: passivePsbts,
	}, nil
}

// jsonSignVirtualPsbtRequest is the JSON body for
// SignVirtualPsbt.
type jsonSignVirtualPsbtRequest struct {
	FundedPsbt string `json:"funded_psbt"`
}

// SignVirtualPsbt signs a virtual transaction.
func (w *walletKitClient) SignVirtualPsbt(ctx context.Context,
	fundedPsbt []byte) ([]byte, error) {

	body := &jsonSignVirtualPsbtRequest{
		FundedPsbt: base64.StdEncoding.EncodeToString(
			fundedPsbt,
		),
	}

	var resp jsonSignVirtualPsbtResponse
	err := w.transport.doPost(
		ctx, "/v1/taproot-assets/wallet/virtual-psbt/sign",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	return parseBase64Bytes(resp.SignedPsbt)
}

// jsonCommitVirtualPsbtsRequest is the JSON body for
// CommitVirtualPsbts.
type jsonCommitVirtualPsbtsRequest struct {
	VirtualPsbts      []string `json:"virtual_psbts"`
	PassiveAssetPsbts []string `json:"passive_asset_psbts,omitempty"`
	SatPerVByte       string   `json:"sat_per_vbyte,omitempty"`
	Add               bool     `json:"add,omitempty"`
}

// CommitVirtualPsbts commits virtual transactions.
func (w *walletKitClient) CommitVirtualPsbts(ctx context.Context,
	virtualPsbts [][]byte, passivePsbts [][]byte,
	feeRate uint64) (*entities.CommittedTransfer, error) {

	vPsbts := make([]string, 0, len(virtualPsbts))
	for _, p := range virtualPsbts {
		vPsbts = append(
			vPsbts,
			base64.StdEncoding.EncodeToString(p),
		)
	}

	pPsbts := make([]string, 0, len(passivePsbts))
	for _, p := range passivePsbts {
		pPsbts = append(
			pPsbts,
			base64.StdEncoding.EncodeToString(p),
		)
	}

	body := &jsonCommitVirtualPsbtsRequest{
		VirtualPsbts:      vPsbts,
		PassiveAssetPsbts: pPsbts,
		SatPerVByte:       fmt.Sprintf("%d", feeRate),
		Add:               true,
	}

	var resp jsonCommitVirtualPsbtsResponse
	err := w.transport.doPost(
		ctx,
		"/v1/taproot-assets/wallet/virtual-psbt/commit",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	anchorPsbt, err := parseBase64Bytes(resp.AnchorPsbt)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid anchor_psbt: %w", err,
		)
	}

	vPsbtBytes := make([][]byte, 0, len(resp.VirtualPsbts))
	for _, p := range resp.VirtualPsbts {
		psbt, err := parseBase64Bytes(p)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid virtual_psbt: %w", err,
			)
		}

		vPsbtBytes = append(vPsbtBytes, psbt)
	}

	pPsbtBytes := make(
		[][]byte, 0, len(resp.PassiveAssetPsbts),
	)
	for _, p := range resp.PassiveAssetPsbts {
		psbt, err := parseBase64Bytes(p)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid passive_psbt: %w", err,
			)
		}

		pPsbtBytes = append(pPsbtBytes, psbt)
	}

	return &entities.CommittedTransfer{
		AnchorPsbt:        anchorPsbt,
		VirtualPsbts:      vPsbtBytes,
		PassiveAssetPsbts: pPsbtBytes,
	}, nil
}

// jsonAnchorVirtualPsbtsRequest is the JSON body for
// AnchorVirtualPsbts.
type jsonAnchorVirtualPsbtsRequest struct {
	VirtualPsbts []string `json:"virtual_psbts"`
}

// AnchorVirtualPsbts anchors signed virtual PSBTs in a single
// call.
func (w *walletKitClient) AnchorVirtualPsbts(ctx context.Context,
	signedPsbts [][]byte) (*entities.AssetTransfer, error) {

	psbts := make([]string, 0, len(signedPsbts))
	for _, p := range signedPsbts {
		psbts = append(
			psbts,
			base64.StdEncoding.EncodeToString(p),
		)
	}

	body := &jsonAnchorVirtualPsbtsRequest{
		VirtualPsbts: psbts,
	}

	var resp jsonSendAssetResponse
	err := w.transport.doPost(
		ctx,
		"/v1/taproot-assets/wallet/virtual-psbt/anchor",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	return unmarshalAssetTransfer(resp.Transfer)
}

// jsonPublishAndLogRequest is the JSON body for
// PublishAndLogTransfer.
type jsonPublishAndLogRequest struct {
	AnchorPsbt        string   `json:"anchor_psbt"`
	VirtualPsbts      []string `json:"virtual_psbts"`
	PassiveAssetPsbts []string `json:"passive_asset_psbts,omitempty"`

	SkipAnchorTxBroadcast bool `json:"skip_anchor_tx_broadcast,omitempty"` //nolint:lll
}

// PublishAndLogTransfer publishes the anchor transaction and logs
// the transfer.
func (w *walletKitClient) PublishAndLogTransfer(ctx context.Context,
	anchorPsbt []byte, virtualPsbts [][]byte,
	passivePsbts [][]byte,
	skipAnchorTxBroadcast bool) (
	*entities.AssetPacket, error) {

	vPsbts := make([]string, 0, len(virtualPsbts))
	for _, p := range virtualPsbts {
		vPsbts = append(
			vPsbts,
			base64.StdEncoding.EncodeToString(p),
		)
	}

	pPsbts := make([]string, 0, len(passivePsbts))
	for _, p := range passivePsbts {
		pPsbts = append(
			pPsbts,
			base64.StdEncoding.EncodeToString(p),
		)
	}

	body := &jsonPublishAndLogRequest{
		AnchorPsbt: base64.StdEncoding.EncodeToString(
			anchorPsbt,
		),
		VirtualPsbts:          vPsbts,
		PassiveAssetPsbts:     pPsbts,
		SkipAnchorTxBroadcast: skipAnchorTxBroadcast,
	}

	var resp jsonPublishAndLogResponse
	err := w.transport.doPost(
		ctx,
		"/v1/taproot-assets/wallet/virtual-psbt/log-transfer",
		macaroon.WalletKitServiceMac, body, &resp,
	)
	if err != nil {
		return nil, err
	}

	if resp.Transfer == nil {
		return nil, fmt.Errorf("invalid transfer response")
	}

	anchorTx, err := parseBase64Bytes(resp.Transfer.AnchorTx)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid anchor_tx: %w", err,
		)
	}

	// Return the raw bytes from the original call for
	// virtual and passive transactions.
	return &entities.AssetPacket{
		AnchorTransaction:        anchorTx,
		VirtualTransactions:      virtualPsbts,
		PassiveAssetTransactions: passivePsbts,
	}, nil
}
