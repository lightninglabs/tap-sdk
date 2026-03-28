package tapsdk

import (
	"context"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockClient is a full mock of the Client interface for testing Wallet
// high-level methods. Only methods needed by the tests are implemented;
// the rest panic to catch unexpected calls.
type mockClient struct {
	mock.Mock
}

// --- WalletClient (TaprootAssets service) ---

func (m *mockClient) GetInfo(ctx context.Context) (*entities.Info, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.Info), args.Error(1)
}

func (m *mockClient) ListAssets(ctx context.Context,
	req *entities.ListAssetsRequest) ([]*entities.Asset, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]*entities.Asset), args.Error(1)
}

func (m *mockClient) ListBalances(ctx context.Context,
	req *entities.ListBalancesRequest) (*entities.ListBalancesResponse,
	error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.ListBalancesResponse), args.Error(1)
}

func (m *mockClient) ListTransfers(ctx context.Context,
	req *entities.ListTransfersRequest) ([]*entities.AssetTransfer, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]*entities.AssetTransfer), args.Error(1)
}

func (m *mockClient) SendAsset(ctx context.Context,
	req *entities.SendAssetRequest) (*entities.AssetTransfer, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.AssetTransfer), args.Error(1)
}

func (m *mockClient) NewAddr(ctx context.Context,
	req *entities.NewAddressRequest) (*entities.Address, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.Address), args.Error(1)
}

func (m *mockClient) DecodeAddr(ctx context.Context,
	addr string) (*entities.Address, error) {

	args := m.Called(ctx, addr)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.Address), args.Error(1)
}

func (m *mockClient) QueryAddrs(ctx context.Context,
	query *entities.AddressQuery) ([]*entities.Address, error) {

	args := m.Called(ctx, query)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]*entities.Address), args.Error(1)
}

func (m *mockClient) AddrReceives(ctx context.Context,
	query *entities.AddressReceivesQuery) ([]*entities.AddressEvent,
	error) {

	args := m.Called(ctx, query)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]*entities.AddressEvent), args.Error(1)
}

// --- ProofClient ---

func (m *mockClient) ExportProof(ctx context.Context,
	assetID entities.AssetID, scriptKey entities.PubKey,
	outpoint *entities.Outpoint) (*entities.ProofFile, error) {

	args := m.Called(ctx, assetID, scriptKey, outpoint)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.ProofFile), args.Error(1)
}

func (m *mockClient) UnpackProofFile(ctx context.Context,
	rawProofFile []byte) ([][]byte, error) {

	args := m.Called(ctx, rawProofFile)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([][]byte), args.Error(1)
}

func (m *mockClient) DecodeProof(ctx context.Context,
	rawProof []byte) (*entities.DecodedProof, error) {

	args := m.Called(ctx, rawProof)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.DecodedProof), args.Error(1)
}

func (m *mockClient) RegisterTransfer(ctx context.Context,
	assetID entities.AssetID, groupKey *entities.PubKey,
	scriptKey entities.PubKey,
	outpoint entities.Outpoint) (*entities.RegisteredAsset, error) {

	args := m.Called(ctx, assetID, groupKey, scriptKey, outpoint)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.RegisteredAsset), args.Error(1)
}

// --- WalletKitClient (AssetWallet service) ---

func (m *mockClient) DeriveScriptKey(
	ctx context.Context) (*entities.ScriptKey, error) {

	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.ScriptKey), args.Error(1)
}

func (m *mockClient) DeriveInternalKey(
	ctx context.Context) (*entities.InternalKey, error) {

	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.InternalKey), args.Error(1)
}

func (m *mockClient) FundTransfer(ctx context.Context,
	recipients []entities.Recipient,
	inputs []entities.PrevID) (*entities.FundedTransfer, error) {

	args := m.Called(ctx, recipients, inputs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.FundedTransfer), args.Error(1)
}

func (m *mockClient) FundInteractivePsbt(ctx context.Context,
	psbt []byte) (*entities.FundedTransfer, error) {

	args := m.Called(ctx, psbt)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.FundedTransfer), args.Error(1)
}

func (m *mockClient) SignVirtualPsbt(ctx context.Context,
	fundedPsbt []byte) ([]byte, error) {

	args := m.Called(ctx, fundedPsbt)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]byte), args.Error(1)
}

func (m *mockClient) CommitVirtualPsbts(ctx context.Context,
	virtualPsbts [][]byte, passivePsbts [][]byte,
	feeRate uint64) (*entities.CommittedTransfer, error) {

	args := m.Called(ctx, virtualPsbts, passivePsbts, feeRate)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.CommittedTransfer), args.Error(1)
}

func (m *mockClient) AnchorVirtualPsbts(ctx context.Context,
	signedPsbts [][]byte) (*entities.AssetTransfer, error) {

	args := m.Called(ctx, signedPsbts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.AssetTransfer), args.Error(1)
}

func (m *mockClient) PublishAndLogTransfer(ctx context.Context,
	anchorPsbt []byte, virtualPsbts [][]byte, passivePsbts [][]byte,
	skipAnchorTxBroadcast bool) (*entities.AssetPacket, error) {

	args := m.Called(ctx, anchorPsbt, virtualPsbts, passivePsbts,
		skipAnchorTxBroadcast)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.AssetPacket), args.Error(1)
}

// --- UniverseClient ---

func (m *mockClient) InsertProof(ctx context.Context, rawProof []byte,
	decoded *entities.DecodedProof) error {

	args := m.Called(ctx, rawProof, decoded)
	return args.Error(0)
}

// --- MintClient ---

func (m *mockClient) MintAsset(ctx context.Context,
	req *entities.MintAssetRequest) (*entities.MintingBatch, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.MintingBatch), args.Error(1)
}

func (m *mockClient) FundBatch(ctx context.Context,
	req *entities.FundBatchRequest) (*entities.VerboseMintingBatch,
	error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.VerboseMintingBatch), args.Error(1)
}

func (m *mockClient) SealBatch(ctx context.Context,
	req *entities.SealBatchRequest) (*entities.MintingBatch, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.MintingBatch), args.Error(1)
}

func (m *mockClient) FinalizeBatch(ctx context.Context,
	req *entities.FinalizeBatchRequest) (*entities.MintingBatch, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.MintingBatch), args.Error(1)
}

func (m *mockClient) CancelBatch(
	ctx context.Context) (*entities.CancelBatchResponse, error) {

	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.CancelBatchResponse), args.Error(1)
}

func (m *mockClient) ListBatches(ctx context.Context,
	req *entities.ListBatchesRequest) ([]*entities.VerboseMintingBatch,
	error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]*entities.VerboseMintingBatch), args.Error(1)
}

func (m *mockClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

// --- Tests ---

func TestSend_WithAmount(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	addr := "taprt1qxyz_v2_address"
	amount := uint64(500)
	feeRate := uint32(25)
	label := "test-send"

	expectedTransfer := &entities.AssetTransfer{
		AnchorTxid: "abc123",
		Label:      label,
	}

	mc.On("SendAsset", ctx, mock.MatchedBy(
		func(req *entities.SendAssetRequest) bool {
			return len(req.Recipients) == 1 &&
				req.Recipients[0].Address == addr &&
				req.Recipients[0].Amount == amount &&
				req.FeeRate == feeRate &&
				req.Label == label &&
				len(req.TapAddresses) == 0
		}),
	).Return(expectedTransfer, nil)

	transfer, err := w.Send(
		ctx, addr, amount,
		WithFeeRate(feeRate), WithLabel(label),
	)
	require.NoError(t, err)
	require.Equal(t, expectedTransfer, transfer)

	mc.AssertExpectations(t)
}

func TestSend_ZeroAmount_UsesAddressEmbedded(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	addr := "taprt1qxyz_v0_address"

	expectedTransfer := &entities.AssetTransfer{
		AnchorTxid: "def456",
	}

	mc.On("SendAsset", ctx, mock.MatchedBy(
		func(req *entities.SendAssetRequest) bool {
			return len(req.TapAddresses) == 1 &&
				req.TapAddresses[0] == addr &&
				len(req.Recipients) == 0
		}),
	).Return(expectedTransfer, nil)

	transfer, err := w.Send(ctx, addr, 0)
	require.NoError(t, err)
	require.Equal(t, expectedTransfer, transfer)

	mc.AssertExpectations(t)
}

func TestSend_Error(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	mc.On("SendAsset", ctx, mock.Anything).Return(
		nil, context.DeadlineExceeded,
	)

	_, err := w.Send(ctx, "taprt1qfoo", 100)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	mc.AssertExpectations(t)
}

func TestSend_WithSkipProofCourierPingCheck(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	expectedTransfer := &entities.AssetTransfer{
		AnchorTxid: "ghi789",
	}

	mc.On("SendAsset", ctx, mock.MatchedBy(
		func(req *entities.SendAssetRequest) bool {
			return req.SkipProofCourierPingCheck
		}),
	).Return(expectedTransfer, nil)

	transfer, err := w.Send(
		ctx, "taprt1qbar", 42,
		WithSkipProofCourierPingCheck(),
	)
	require.NoError(t, err)
	require.Equal(t, expectedTransfer, transfer)

	mc.AssertExpectations(t)
}

func TestSendMulti_MultipleRecipients(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	recipients := []entities.Recipient{
		{Address: "taprt1qalice", Amount: 100},
		{Address: "taprt1qbob", Amount: 200},
	}

	expectedTransfer := &entities.AssetTransfer{
		AnchorTxid: "multi123",
	}

	mc.On("SendAsset", ctx, mock.MatchedBy(
		func(req *entities.SendAssetRequest) bool {
			return len(req.Recipients) == 2 &&
				req.Recipients[0].Address == "taprt1qalice" &&
				req.Recipients[0].Amount == 100 &&
				req.Recipients[1].Address == "taprt1qbob" &&
				req.Recipients[1].Amount == 200
		}),
	).Return(expectedTransfer, nil)

	transfer, err := w.SendMulti(ctx, recipients)
	require.NoError(t, err)
	require.Equal(t, expectedTransfer, transfer)

	mc.AssertExpectations(t)
}

func TestSendMulti_EmbeddedAmounts(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	recipients := []entities.Recipient{
		{Address: "taprt1qalice_v0"},
		{Address: "taprt1qbob_v0"},
	}

	expectedTransfer := &entities.AssetTransfer{
		AnchorTxid: "multi_v0",
	}

	mc.On("SendAsset", ctx, mock.MatchedBy(
		func(req *entities.SendAssetRequest) bool {
			return len(req.TapAddresses) == 2 &&
				req.TapAddresses[0] == "taprt1qalice_v0" &&
				req.TapAddresses[1] == "taprt1qbob_v0" &&
				len(req.Recipients) == 0
		}),
	).Return(expectedTransfer, nil)

	transfer, err := w.SendMulti(ctx, recipients)
	require.NoError(t, err)
	require.Equal(t, expectedTransfer, transfer)

	mc.AssertExpectations(t)
}

func TestSendMulti_NoRecipients(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	_, err := w.SendMulti(ctx, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoRecipients)

	_, err = w.SendMulti(ctx, []entities.Recipient{})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoRecipients)
}

func TestSendMulti_WithOptions(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	recipients := []entities.Recipient{
		{Address: "taprt1qalice", Amount: 50},
	}

	expectedTransfer := &entities.AssetTransfer{
		AnchorTxid: "opts123",
		Label:      "batch-payment",
	}

	mc.On("SendAsset", ctx, mock.MatchedBy(
		func(req *entities.SendAssetRequest) bool {
			return req.FeeRate == 10 &&
				req.Label == "batch-payment" &&
				req.SkipProofCourierPingCheck
		}),
	).Return(expectedTransfer, nil)

	transfer, err := w.SendMulti(
		ctx, recipients,
		WithFeeRate(10),
		WithLabel("batch-payment"),
		WithSkipProofCourierPingCheck(),
	)
	require.NoError(t, err)
	require.Equal(t, expectedTransfer, transfer)

	mc.AssertExpectations(t)
}
