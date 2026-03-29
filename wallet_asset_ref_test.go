package tapsdk

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// refMockClient is a full mock of the Client interface for testing AssetRef-
// based Wallet methods. Methods not exercised by these tests use panic
// stubs so unexpected calls are caught immediately.
type refMockClient struct {
	mock.Mock
}

// --- WalletClient ---

func (m *refMockClient) GetInfo(ctx context.Context) (*entities.Info,
	error) {

	panic("GetInfo: unexpected call")
}

func (m *refMockClient) ListAssets(ctx context.Context,
	req *entities.ListAssetsRequest) ([]*entities.Asset, error) {

	panic("ListAssets: unexpected call")
}

func (m *refMockClient) ListBalances(ctx context.Context,
	req *entities.ListBalancesRequest) (*entities.ListBalancesResponse,
	error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.ListBalancesResponse), args.Error(1)
}

func (m *refMockClient) ListTransfers(ctx context.Context,
	req *entities.ListTransfersRequest) ([]*entities.AssetTransfer,
	error) {

	panic("ListTransfers: unexpected call")
}

func (m *refMockClient) SendAsset(ctx context.Context,
	req *entities.SendAssetRequest) (*entities.AssetTransfer, error) {

	panic("SendAsset: unexpected call")
}

func (m *refMockClient) NewAddr(ctx context.Context,
	req *entities.NewAddressRequest) (*entities.Address, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.Address), args.Error(1)
}

func (m *refMockClient) DecodeAddr(ctx context.Context,
	addr string) (*entities.Address, error) {

	panic("DecodeAddr: unexpected call")
}

func (m *refMockClient) QueryAddrs(ctx context.Context,
	query *entities.AddressQuery) ([]*entities.Address, error) {

	panic("QueryAddrs: unexpected call")
}

func (m *refMockClient) AddrReceives(ctx context.Context,
	query *entities.AddressReceivesQuery) ([]*entities.AddressEvent,
	error) {

	panic("AddrReceives: unexpected call")
}

// --- ProofClient ---

func (m *refMockClient) ExportProof(ctx context.Context,
	assetID entities.AssetID, scriptKey entities.PubKey,
	outpoint *entities.Outpoint) (*entities.ProofFile, error) {

	panic("ExportProof: unexpected call")
}

func (m *refMockClient) UnpackProofFile(ctx context.Context,
	rawProofFile []byte) ([][]byte, error) {

	panic("UnpackProofFile: unexpected call")
}

func (m *refMockClient) DecodeProof(ctx context.Context,
	rawProof []byte) (*entities.DecodedProof, error) {

	panic("DecodeProof: unexpected call")
}

func (m *refMockClient) RegisterTransfer(ctx context.Context,
	assetID entities.AssetID, groupKey *entities.PubKey,
	scriptKey entities.PubKey,
	outpoint entities.Outpoint) (*entities.RegisteredAsset, error) {

	panic("RegisterTransfer: unexpected call")
}

// --- WalletKitClient ---

func (m *refMockClient) DeriveScriptKey(
	ctx context.Context) (*entities.ScriptKey, error) {

	panic("DeriveScriptKey: unexpected call")
}

func (m *refMockClient) DeriveInternalKey(
	ctx context.Context) (*entities.InternalKey, error) {

	panic("DeriveInternalKey: unexpected call")
}

func (m *refMockClient) FundTransfer(ctx context.Context,
	recipients []entities.Recipient,
	inputs []entities.PrevID) (*entities.FundedTransfer, error) {

	panic("FundTransfer: unexpected call")
}

func (m *refMockClient) FundInteractivePsbt(ctx context.Context,
	psbt []byte) (*entities.FundedTransfer, error) {

	panic("FundInteractivePsbt: unexpected call")
}

func (m *refMockClient) SignVirtualPsbt(ctx context.Context,
	fundedPsbt []byte) ([]byte, error) {

	panic("SignVirtualPsbt: unexpected call")
}

func (m *refMockClient) CommitVirtualPsbts(ctx context.Context,
	virtualPsbts [][]byte, passivePsbts [][]byte,
	feeRate uint64) (*entities.CommittedTransfer, error) {

	panic("CommitVirtualPsbts: unexpected call")
}

func (m *refMockClient) AnchorVirtualPsbts(ctx context.Context,
	signedPsbts [][]byte) (*entities.AssetTransfer, error) {

	panic("AnchorVirtualPsbts: unexpected call")
}

func (m *refMockClient) PublishAndLogTransfer(ctx context.Context,
	anchorPsbt []byte, virtualPsbts [][]byte, passivePsbts [][]byte,
	skipAnchorTxBroadcast bool) (*entities.AssetPacket, error) {

	panic("PublishAndLogTransfer: unexpected call")
}

// --- UniverseClient ---

func (m *refMockClient) InsertProof(ctx context.Context,
	rawProof []byte, decoded *entities.DecodedProof) error {

	panic("InsertProof: unexpected call")
}

// --- MintClient ---

func (m *refMockClient) MintAsset(ctx context.Context,
	req *entities.MintAssetRequest) (*entities.MintingBatch, error) {

	panic("MintAsset: unexpected call")
}

func (m *refMockClient) FundBatch(ctx context.Context,
	req *entities.FundBatchRequest) (*entities.VerboseMintingBatch,
	error) {

	panic("FundBatch: unexpected call")
}

func (m *refMockClient) SealBatch(ctx context.Context,
	req *entities.SealBatchRequest) (*entities.MintingBatch, error) {

	panic("SealBatch: unexpected call")
}

func (m *refMockClient) FinalizeBatch(ctx context.Context,
	req *entities.FinalizeBatchRequest) (*entities.MintingBatch, error) {

	panic("FinalizeBatch: unexpected call")
}

func (m *refMockClient) CancelBatch(
	ctx context.Context) (*entities.CancelBatchResponse, error) {

	panic("CancelBatch: unexpected call")
}

func (m *refMockClient) ListBatches(ctx context.Context,
	req *entities.ListBatchesRequest) ([]*entities.VerboseMintingBatch,
	error) {

	panic("ListBatches: unexpected call")
}

func (m *refMockClient) Close() error {
	return nil
}

// --- Test helpers ---

func testRefGroupKey(t *testing.T) entities.PubKey {
	t.Helper()

	b, err := hex.DecodeString(
		"02a1633cafcc01ebfb6d78e39f687a1f0995c62fc95f51ead10" +
			"a02ee0be551b5dc",
	)
	require.NoError(t, err)

	key, err := entities.ParsePubKey(b)
	require.NoError(t, err)

	return key
}

func testRefAssetID() entities.AssetID {
	var id entities.AssetID
	for i := range id {
		id[i] = byte(i)
	}

	return id
}

// --- NewReceiveAddress tests ---

func TestNewReceiveAddress_Fungible(t *testing.T) {
	mc := new(refMockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	groupKey := testRefGroupKey(t)
	ref := entities.AssetRefFromGroupKey(groupKey)

	expectedAddr := &entities.Address{
		Encoded:        "taprt1q_fungible_test",
		GroupKey:       &groupKey,
		AddressVersion: entities.AddressVersionV2,
	}

	v2 := entities.AddressVersionV2
	mc.On("NewAddr", ctx, &entities.NewAddressRequest{
		GroupKey:       &groupKey,
		AddressVersion: &v2,
	}).Return(expectedAddr, nil)

	addr, err := w.NewReceiveAddress(ctx, ref)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, addr)

	mc.AssertExpectations(t)
}

func TestNewReceiveAddress_Collectible(t *testing.T) {
	mc := new(refMockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	assetID := testRefAssetID()
	ref := entities.AssetRefFromAssetID(assetID)

	expectedAddr := &entities.Address{
		Encoded:        "taprt1q_collectible_test",
		AssetID:        assetID,
		AddressVersion: entities.AddressVersionV2,
	}

	v2 := entities.AddressVersionV2
	mc.On("NewAddr", ctx, &entities.NewAddressRequest{
		AssetID:        &assetID,
		AddressVersion: &v2,
	}).Return(expectedAddr, nil)

	addr, err := w.NewReceiveAddress(ctx, ref)
	require.NoError(t, err)
	require.Equal(t, expectedAddr, addr)

	mc.AssertExpectations(t)
}

func TestNewReceiveAddress_Error(t *testing.T) {
	mc := new(refMockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	ref := entities.AssetRefFromGroupKey(testRefGroupKey(t))

	mc.On("NewAddr", ctx, mock.Anything).Return(
		nil, fmt.Errorf("connection refused"),
	)

	_, err := w.NewReceiveAddress(ctx, ref)
	require.Error(t, err)
	require.Contains(t, err.Error(), "connection refused")

	mc.AssertExpectations(t)
}

// --- GetBalance tests ---

func TestGetBalance_Fungible(t *testing.T) {
	mc := new(refMockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	groupKey := testRefGroupKey(t)
	ref := entities.AssetRefFromGroupKey(groupKey)

	mc.On("ListBalances", ctx, &entities.ListBalancesRequest{
		GroupBy:        entities.BalanceGroupByGroupKey,
		GroupKeyFilter: &groupKey,
	}).Return(&entities.ListBalancesResponse{
		AssetGroupBalances: map[string]*entities.AssetGroupBalance{
			groupKey.String(): {
				GroupKey: &groupKey,
				Balance:  42000,
			},
		},
	}, nil)

	balance, err := w.GetBalance(ctx, ref)
	require.NoError(t, err)
	require.Equal(t, uint64(42000), balance)

	mc.AssertExpectations(t)
}

func TestGetBalance_Collectible(t *testing.T) {
	mc := new(refMockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	assetID := testRefAssetID()
	ref := entities.AssetRefFromAssetID(assetID)

	mc.On("ListBalances", ctx, &entities.ListBalancesRequest{
		GroupBy:     entities.BalanceGroupByAssetID,
		AssetFilter: &assetID,
	}).Return(&entities.ListBalancesResponse{
		AssetBalances: map[string]*entities.AssetBalance{
			assetID.String(): {
				Balance: 1,
			},
		},
	}, nil)

	balance, err := w.GetBalance(ctx, ref)
	require.NoError(t, err)
	require.Equal(t, uint64(1), balance)

	mc.AssertExpectations(t)
}

func TestGetBalance_ZeroWhenEmpty(t *testing.T) {
	mc := new(refMockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	groupKey := testRefGroupKey(t)
	ref := entities.AssetRefFromGroupKey(groupKey)

	mc.On("ListBalances", ctx, mock.Anything).Return(
		&entities.ListBalancesResponse{
			AssetGroupBalances: map[string]*entities.AssetGroupBalance{},
		}, nil,
	)

	balance, err := w.GetBalance(ctx, ref)
	require.NoError(t, err)
	require.Equal(t, uint64(0), balance)

	mc.AssertExpectations(t)
}

func TestGetBalance_Error(t *testing.T) {
	mc := new(refMockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	ref := entities.AssetRefFromAssetID(testRefAssetID())

	mc.On("ListBalances", ctx, mock.Anything).Return(
		nil, context.DeadlineExceeded,
	)

	_, err := w.GetBalance(ctx, ref)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	mc.AssertExpectations(t)
}
