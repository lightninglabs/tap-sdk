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

func (m *refMockClient) ListAssetRecords(ctx context.Context,
	req *entities.ListAssetsRequest) ([]*entities.AssetRecord, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]*entities.AssetRecord), args.Error(1)
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
	ref entities.AssetRef, scriptKey entities.PubKey,
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
	assetRef entities.AssetRef, scriptKey entities.PubKey,
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

func (m *refMockClient) CreateAsset(ctx context.Context,
	req *entities.CreateAssetRequest) (*entities.MintingBatch, error) {

	panic("CreateAsset: unexpected call")
}

func (m *refMockClient) CreateIssuance(ctx context.Context,
	req *entities.CreateIssuanceRequest) (*entities.MintingBatch, error) {

	panic("CreateIssuance: unexpected call")
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

// --- WalletClient extras (PR #30 additions) ---

func (m *refMockClient) ListUtxos(_ context.Context,
	_ *entities.ListUtxosRequest) (
	map[string]*entities.ManagedUtxo, error) {

	return nil, nil
}

func (m *refMockClient) ListAssetGroups(
	_ context.Context) ([]entities.AssetGroupRecord, error) {

	return nil, nil
}

func (m *refMockClient) BurnAsset(_ context.Context,
	_ *entities.BurnAssetRequest) (
	*entities.BurnAssetResponse, error) {

	return nil, nil
}

func (m *refMockClient) ListBurns(_ context.Context,
	_ *entities.ListBurnsRequest) ([]*entities.AssetBurn, error) {

	return nil, nil
}

func (m *refMockClient) FetchAssetMeta(_ context.Context,
	_ *entities.FetchAssetMetaRequest) (
	*entities.AssetMeta, error) {

	return nil, nil
}

func (m *refMockClient) VerifyProof(_ context.Context,
	_ []byte) (*entities.VerifyProofResponse, error) {

	return nil, nil
}

// --- WalletKitClient extras (PR #30 additions) ---

func (m *refMockClient) QueryInternalKey(_ context.Context,
	_ []byte) (*entities.KeyDescriptor, error) {

	return nil, nil
}

func (m *refMockClient) QueryScriptKey(_ context.Context,
	_ []byte) (*entities.ScriptKey, error) {

	return nil, nil
}

func (m *refMockClient) ProveAssetOwnership(_ context.Context,
	_ *entities.ProveOwnershipRequest) (
	*entities.OwnershipProof, error) {

	return nil, nil
}

func (m *refMockClient) VerifyAssetOwnership(_ context.Context,
	_ *entities.VerifyOwnershipRequest) (
	*entities.VerifyOwnershipResponse, error) {

	return nil, nil
}

func (m *refMockClient) RemoveUTXOLease(_ context.Context,
	_ entities.Outpoint) error {

	return nil
}

func (m *refMockClient) DeclareScriptKey(_ context.Context,
	_ *entities.DeclareScriptKeyRequest) (
	*entities.ScriptKey, error) {

	return nil, nil
}

func (m *refMockClient) ExportBackup(_ context.Context,
	_ entities.BackupMode) ([]byte, error) {

	return nil, nil
}

func (m *refMockClient) ImportBackup(_ context.Context,
	_ []byte) (uint32, error) {

	return 0, nil
}

// --- UniverseClient (PR #31 additions) ---

func (m *refMockClient) AssetRoots(_ context.Context,
	_ *entities.AssetRootRequest) (
	map[string]*entities.UniverseRoot, error) {

	return nil, nil
}

func (m *refMockClient) QueryAssetRoots(ctx context.Context,
	id *entities.UniverseID) (*entities.QueryRootResponse, error) {

	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.QueryRootResponse), args.Error(1)
}

func (m *refMockClient) DeleteAssetRoot(_ context.Context,
	_ *entities.UniverseID) error {

	return nil
}

func (m *refMockClient) AssetLeafKeys(_ context.Context,
	_ *entities.AssetLeafKeysRequest) (
	[]entities.AssetLeafKey, error) {

	return nil, nil
}

func (m *refMockClient) AssetLeaves(_ context.Context,
	_ *entities.UniverseID) ([]entities.AssetLeaf, error) {

	return nil, nil
}

func (m *refMockClient) QueryProof(_ context.Context,
	_ *entities.UniverseKey) (*entities.AssetProofResponse,
	error) {

	return nil, nil
}

func (m *refMockClient) UniverseStats(
	_ context.Context) (*entities.UniverseStats, error) {

	return nil, nil
}

func (m *refMockClient) QueryAssetStats(_ context.Context,
	_ *entities.AssetStatsQuery) (
	[]entities.AssetStatsSnapshot, error) {

	return nil, nil
}

func (m *refMockClient) QueryEvents(_ context.Context,
	_ *entities.QueryEventsRequest) (
	[]entities.GroupedUniverseEvents, error) {

	return nil, nil
}

func (m *refMockClient) ListFederationServers(
	_ context.Context) ([]entities.FederationServer, error) {

	return nil, nil
}

func (m *refMockClient) AddFederationServer(_ context.Context,
	_ []entities.FederationServer) error {

	return nil
}

func (m *refMockClient) DeleteFederationServer(_ context.Context,
	_ []entities.FederationServer) error {

	return nil
}

func (m *refMockClient) SetFederationSyncConfig(_ context.Context,
	_ []entities.GlobalFederationSyncConfig,
	_ []entities.AssetFederationSyncConfig) error {

	return nil
}

func (m *refMockClient) QueryFederationSyncConfig(_ context.Context,
	_ []entities.UniverseID) (*entities.FederationSyncConfig,
	error) {

	return nil, nil
}

func (m *refMockClient) Info(
	_ context.Context) (*entities.UniverseInfo, error) {

	return nil, nil
}

func (m *refMockClient) SyncUniverse(_ context.Context,
	_ *entities.SyncRequest) ([]entities.SyncedUniverse, error) {

	return nil, nil
}

func (m *refMockClient) SubscribeReceiveEvents(_ context.Context,
	_ *entities.SubscribeReceiveEventsRequest) (
	<-chan *entities.ReceiveEvent, <-chan error, error) {

	panic("SubscribeReceiveEvents: unexpected call")
}

func (m *refMockClient) SubscribeSendEvents(_ context.Context,
	_ *entities.SubscribeSendEventsRequest) (
	<-chan *entities.SendEvent, <-chan error, error) {

	panic("SubscribeSendEvents: unexpected call")
}

func (m *refMockClient) SubscribeMintEvents(_ context.Context,
	_ *entities.SubscribeMintEventsRequest) (
	<-chan *entities.MintEvent, <-chan error, error) {

	panic("SubscribeMintEvents: unexpected call")
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
		AssetRef:       ref,
		AddressVersion: entities.AddressVersionV2,
	}

	v2 := entities.AddressVersionV2
	mc.On("NewAddr", ctx, &entities.NewAddressRequest{
		AssetRef:       ref,
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
		AssetRef:       ref,
		AddressVersion: entities.AddressVersionV2,
	}

	v2 := entities.AddressVersionV2
	mc.On("NewAddr", ctx, &entities.NewAddressRequest{
		AssetRef:       ref,
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
		AssetRef: &ref,
	}).Return(&entities.ListBalancesResponse{
		Balances: map[string]*entities.Balance{
			ref.String(): {
				AssetRef: ref,
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
		AssetRef: &ref,
	}).Return(&entities.ListBalancesResponse{
		Balances: map[string]*entities.Balance{
			ref.String(): {
				AssetRef: ref,
				Balance:  1,
			},
		},
	}, nil)

	balance, err := w.GetBalance(ctx, ref)
	require.NoError(t, err)
	require.Equal(t, uint64(1), balance)

	mc.AssertExpectations(t)
}

// expectEmptyBalances stubs a ListBalances call that returns no entries
// for the given ref — the cold path all subsequent probes are keyed off.
func expectEmptyBalances(mc *refMockClient, ctx context.Context,
	ref entities.AssetRef) {

	mc.On("ListBalances", ctx, &entities.ListBalancesRequest{
		AssetRef: &ref,
	}).Return(&entities.ListBalancesResponse{
		Balances: map[string]*entities.Balance{},
	}, nil)
}

// issuanceRootID is the UniverseID the cold-path probe consults.
func issuanceRootID(ref entities.AssetRef) *entities.UniverseID {
	return &entities.UniverseID{
		AssetRef:  ref,
		ProofType: entities.ProofTypeIssuance,
	}
}

// TestGetBalance_ZeroWhenUniverseKnown covers the motivating scenario
// from issue #69: a group ref bootstrapped via SyncUniverse but with no
// received units yet. The issuance root resolves the ref as "known".
func TestGetBalance_ZeroWhenUniverseKnown(t *testing.T) {
	mc := new(refMockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	ref := entities.AssetRefFromGroupKey(testRefGroupKey(t))

	expectEmptyBalances(mc, ctx, ref)
	mc.On("QueryAssetRoots", ctx, issuanceRootID(ref)).Return(
		&entities.QueryRootResponse{
			IssuanceRoot: &entities.UniverseRoot{
				ID:        *issuanceRootID(ref),
				AssetName: "bootstrapped",
			},
		}, nil,
	)

	balance, err := w.GetBalance(ctx, ref)
	require.NoError(t, err)
	require.Zero(t, balance)

	mc.AssertExpectations(t)
}

// TestGetBalance_ZeroWhenUniverseKnowsViaTransfer covers the case
// where the wallet has seen transfer proofs for the ref but no
// issuance root is locally available.
func TestGetBalance_ZeroWhenUniverseKnowsViaTransfer(t *testing.T) {
	mc := new(refMockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	ref := entities.AssetRefFromAssetID(testRefAssetID())

	expectEmptyBalances(mc, ctx, ref)
	mc.On("QueryAssetRoots", ctx, issuanceRootID(ref)).Return(
		&entities.QueryRootResponse{
			TransferRoot: &entities.UniverseRoot{
				ID:        *issuanceRootID(ref),
				AssetName: "transfer-only",
			},
		}, nil,
	)

	balance, err := w.GetBalance(ctx, ref)
	require.NoError(t, err)
	require.Zero(t, balance)

	mc.AssertExpectations(t)
}

// TestGetBalance_UnknownAsset covers a ref the wallet has truly never
// seen: no balance entry and no universe root. The error chain carries
// the ref for debugging.
func TestGetBalance_UnknownAsset(t *testing.T) {
	mc := new(refMockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	ref := entities.AssetRefFromGroupKey(testRefGroupKey(t))

	expectEmptyBalances(mc, ctx, ref)
	mc.On("QueryAssetRoots", ctx, issuanceRootID(ref)).Return(
		&entities.QueryRootResponse{}, nil,
	)

	balance, err := w.GetBalance(ctx, ref)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrAssetUnknown)
	require.Contains(t, err.Error(), ref.String())
	require.Zero(t, balance)

	mc.AssertExpectations(t)
}

func TestGetBalance_ListBalancesError(t *testing.T) {
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

func TestGetBalance_QueryAssetRootsError(t *testing.T) {
	mc := new(refMockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	ref := entities.AssetRefFromAssetID(testRefAssetID())

	expectEmptyBalances(mc, ctx, ref)
	mc.On("QueryAssetRoots", ctx, issuanceRootID(ref)).Return(
		nil, context.DeadlineExceeded,
	)

	_, err := w.GetBalance(ctx, ref)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	mc.AssertExpectations(t)
}
