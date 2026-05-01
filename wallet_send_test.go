package tapsdk

import (
	"context"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
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

func (m *mockClient) ListAssetRecords(ctx context.Context,
	req *entities.ListAssetsRequest) ([]*entities.AssetRecord, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]*entities.AssetRecord), args.Error(1)
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
	ref entities.AssetRef, scriptKey entities.PubKey,
	outpoint *entities.Outpoint) (*entities.ProofFile, error) {

	args := m.Called(ctx, ref, scriptKey, outpoint)
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
	assetRef entities.AssetRef, scriptKey entities.PubKey,
	outpoint entities.Outpoint) (*entities.RegisteredAsset, error) {

	args := m.Called(ctx, assetRef, scriptKey, outpoint)
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

func (m *mockClient) MintIssuance(ctx context.Context,
	req *entities.MintIssuanceRequest) (*entities.MintingBatch, error) {

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

// --- WalletClient extras (PR #30 additions) ---

func (m *mockClient) ListUtxos(ctx context.Context,
	req *entities.ListUtxosRequest) (
	map[string]*entities.ManagedUtxo, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(map[string]*entities.ManagedUtxo),
		args.Error(1)
}

func (m *mockClient) ListAssetGroups(
	ctx context.Context) ([]entities.AssetGroupRecord, error) {

	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]entities.AssetGroupRecord), args.Error(1)
}

func (m *mockClient) BurnAsset(ctx context.Context,
	req *entities.BurnAssetRequest) (
	*entities.BurnAssetResponse, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.BurnAssetResponse), args.Error(1)
}

func (m *mockClient) ListBurns(ctx context.Context,
	req *entities.ListBurnsRequest) ([]*entities.AssetBurn, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]*entities.AssetBurn), args.Error(1)
}

func (m *mockClient) FetchAssetMeta(ctx context.Context,
	req *entities.FetchAssetMetaRequest) (
	*entities.AssetMeta, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.AssetMeta), args.Error(1)
}

func (m *mockClient) VerifyProof(ctx context.Context,
	rawProofFile []byte) (
	*entities.VerifyProofResponse, error) {

	args := m.Called(ctx, rawProofFile)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.VerifyProofResponse), args.Error(1)
}

// --- WalletKitClient extras (PR #30 additions) ---

func (m *mockClient) QueryInternalKey(ctx context.Context,
	internalKey []byte) (*entities.KeyDescriptor, error) {

	args := m.Called(ctx, internalKey)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.KeyDescriptor), args.Error(1)
}

func (m *mockClient) QueryScriptKey(ctx context.Context,
	tweakedScriptKey []byte) (*entities.ScriptKey, error) {

	args := m.Called(ctx, tweakedScriptKey)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.ScriptKey), args.Error(1)
}

func (m *mockClient) ProveAssetOwnership(ctx context.Context,
	req *entities.ProveOwnershipRequest) (
	*entities.OwnershipProof, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.OwnershipProof), args.Error(1)
}

func (m *mockClient) VerifyAssetOwnership(ctx context.Context,
	req *entities.VerifyOwnershipRequest) (
	*entities.VerifyOwnershipResponse, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.VerifyOwnershipResponse),
		args.Error(1)
}

func (m *mockClient) RemoveUTXOLease(ctx context.Context,
	outpoint entities.Outpoint) error {

	args := m.Called(ctx, outpoint)
	return args.Error(0)
}

func (m *mockClient) DeclareScriptKey(ctx context.Context,
	req *entities.DeclareScriptKeyRequest) (
	*entities.ScriptKey, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.ScriptKey), args.Error(1)
}

func (m *mockClient) ExportBackup(ctx context.Context,
	mode entities.BackupMode) ([]byte, error) {

	args := m.Called(ctx, mode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]byte), args.Error(1)
}

func (m *mockClient) ImportBackup(ctx context.Context,
	backup []byte) (uint32, error) {

	args := m.Called(ctx, backup)
	return uint32(args.Int(0)), args.Error(1)
}

// --- UniverseClient (PR #31 additions) ---

func (m *mockClient) AssetRoots(ctx context.Context,
	req *entities.AssetRootRequest) (
	map[string]*entities.UniverseRoot, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(map[string]*entities.UniverseRoot),
		args.Error(1)
}

func (m *mockClient) QueryAssetRoots(ctx context.Context,
	id *entities.UniverseID) (*entities.QueryRootResponse, error) {

	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.QueryRootResponse), args.Error(1)
}

func (m *mockClient) DeleteAssetRoot(ctx context.Context,
	id *entities.UniverseID) error {

	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockClient) AssetLeafKeys(ctx context.Context,
	req *entities.AssetLeafKeysRequest) (
	[]entities.AssetLeafKey, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]entities.AssetLeafKey), args.Error(1)
}

func (m *mockClient) AssetLeaves(ctx context.Context,
	id *entities.UniverseID) ([]entities.AssetLeaf, error) {

	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]entities.AssetLeaf), args.Error(1)
}

func (m *mockClient) QueryProof(ctx context.Context,
	key *entities.UniverseKey) (*entities.AssetProofResponse,
	error) {

	args := m.Called(ctx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.AssetProofResponse), args.Error(1)
}

func (m *mockClient) UniverseStats(
	ctx context.Context) (*entities.UniverseStats, error) {

	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.UniverseStats), args.Error(1)
}

func (m *mockClient) QueryAssetStats(ctx context.Context,
	req *entities.AssetStatsQuery) (
	[]entities.AssetStatsSnapshot, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]entities.AssetStatsSnapshot), args.Error(1)
}

func (m *mockClient) QueryEvents(ctx context.Context,
	req *entities.QueryEventsRequest) (
	[]entities.GroupedUniverseEvents, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]entities.GroupedUniverseEvents),
		args.Error(1)
}

func (m *mockClient) ListFederationServers(
	ctx context.Context) ([]entities.FederationServer, error) {

	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]entities.FederationServer), args.Error(1)
}

func (m *mockClient) AddFederationServer(ctx context.Context,
	servers []entities.FederationServer) error {

	args := m.Called(ctx, servers)
	return args.Error(0)
}

func (m *mockClient) DeleteFederationServer(ctx context.Context,
	servers []entities.FederationServer) error {

	args := m.Called(ctx, servers)
	return args.Error(0)
}

func (m *mockClient) SetFederationSyncConfig(ctx context.Context,
	global []entities.GlobalFederationSyncConfig,
	asset []entities.AssetFederationSyncConfig) error {

	args := m.Called(ctx, global, asset)
	return args.Error(0)
}

func (m *mockClient) QueryFederationSyncConfig(ctx context.Context,
	ids []entities.UniverseID) (*entities.FederationSyncConfig,
	error) {

	args := m.Called(ctx, ids)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.FederationSyncConfig),
		args.Error(1)
}

func (m *mockClient) Info(
	ctx context.Context) (*entities.UniverseInfo, error) {

	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.UniverseInfo), args.Error(1)
}

func (m *mockClient) SyncUniverse(ctx context.Context,
	req *entities.SyncRequest) ([]entities.SyncedUniverse, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]entities.SyncedUniverse), args.Error(1)
}

// --- EventClient ---

func (m *mockClient) SubscribeReceiveEvents(ctx context.Context,
	req *entities.SubscribeReceiveEventsRequest) (
	<-chan *entities.ReceiveEvent, <-chan error, error) {

	panic("SubscribeReceiveEvents not expected in unit tests")
}

func (m *mockClient) SubscribeSendEvents(ctx context.Context,
	req *entities.SubscribeSendEventsRequest) (
	<-chan *entities.SendEvent, <-chan error, error) {

	panic("SubscribeSendEvents not expected in unit tests")
}

func (m *mockClient) SubscribeMintEvents(ctx context.Context,
	req *entities.SubscribeMintEventsRequest) (
	<-chan *entities.MintEvent, <-chan error, error) {

	panic("SubscribeMintEvents not expected in unit tests")
}

func (m *mockClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

// --- Tests ---

// amountp returns a pointer to its uint64 argument, making
// entities.Recipient literals compact in tests.
func amountp(n uint64) *uint64 { return &n }

// testKey derives a valid compressed secp256k1 public key from a small
// scalar, so fixture addresses the SDK decodes locally actually parse.
func testKey(t *testing.T, scalar byte) entities.PubKey {
	t.Helper()

	priv, _ := btcec.PrivKeyFromBytes([]byte{scalar})
	pk, err := entities.ParsePubKey(priv.PubKey().SerializeCompressed())
	require.NoError(t, err)
	return pk
}

// testAssetID returns a deterministic asset ID for fixture addresses.
func testAssetID() entities.AssetID {
	var id entities.AssetID
	for i := range id {
		id[i] = byte(i + 1)
	}
	return id
}

// encodeV2NoAmount builds a real bech32m-encoded V2 Tap address with
// no embedded amount so Wallet.Send's local decoder actually parses.
// seed distinguishes otherwise-identical addresses across recipients
// in a single test.
func encodeV2NoAmount(t *testing.T, seed ...byte) string {
	t.Helper()

	s := byte(13)
	if len(seed) == 1 {
		s = seed[0]
	}

	addr := &entities.Address{
		AddressVersion:   entities.AddressVersionV2,
		AssetVersion:     entities.AssetVersionV0,
		AssetRef:         entities.AssetRefFromGroupKey(testKey(t, s)),
		ScriptKey:        testKey(t, 7),
		InternalKey:      testKey(t, 11),
		ProofCourierAddr: "authmailbox+universerpc://localhost:10029",
	}

	encoded, err := entities.EncodeAddress(
		addr, entities.NetworkRegtest,
	)
	require.NoError(t, err)
	return encoded
}

func encodeV2NoAmountForRef(t *testing.T, ref entities.AssetRef,
	seed byte) string {

	t.Helper()

	addr := &entities.Address{
		AddressVersion:   entities.AddressVersionV2,
		AssetVersion:     entities.AssetVersionV0,
		AssetRef:         ref,
		ScriptKey:        testKey(t, seed),
		InternalKey:      testKey(t, seed+1),
		ProofCourierAddr: "authmailbox+universerpc://localhost:10029",
	}

	encoded, err := entities.EncodeAddress(
		addr, entities.NetworkRegtest,
	)
	require.NoError(t, err)
	return encoded
}

func encodeV2EmbeddedForRef(t *testing.T, ref entities.AssetRef, amount uint64,
	seed byte) string {

	t.Helper()

	addr := &entities.Address{
		AddressVersion:   entities.AddressVersionV2,
		AssetVersion:     entities.AssetVersionV0,
		AssetRef:         ref,
		Amount:           amount,
		ScriptKey:        testKey(t, seed),
		InternalKey:      testKey(t, seed+1),
		ProofCourierAddr: "authmailbox+universerpc://localhost:10029",
	}

	encoded, err := entities.EncodeAddress(
		addr, entities.NetworkRegtest,
	)
	require.NoError(t, err)
	return encoded
}

// encodeEmbedded builds an address carrying an embedded amount so the
// legacy RecipientsV1 path is exercised.
func encodeEmbedded(t *testing.T, amount uint64,
	version entities.AddressVersion) string {

	t.Helper()

	s := byte(17)

	id := testAssetID()
	// Mutate the first byte so the AssetRef differs between seeds.
	id[0] = s

	addr := &entities.Address{
		AddressVersion: version,
		AssetVersion:   entities.AssetVersionV0,
		AssetRef:       entities.AssetRefFromAssetID(id),
		Amount:         amount,
		ScriptKey:      testKey(t, 7),
		InternalKey:    testKey(t, 11),
	}

	encoded, err := entities.EncodeAddress(
		addr, entities.NetworkRegtest,
	)
	require.NoError(t, err)
	return encoded
}

func TestSend_WithAmount(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	addr := encodeV2NoAmount(t)
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
				req.Recipients[0].Amount != nil &&
				*req.Recipients[0].Amount == amount &&
				req.FeeRate == feeRate &&
				req.Label == label
		}),
	).Return(expectedTransfer, nil)

	transfer, err := w.Send(
		ctx, addr,
		WithAmount(amount),
		WithFeeRate(feeRate), WithLabel(label),
	)
	require.NoError(t, err)
	require.Equal(t, expectedTransfer, transfer)

	mc.AssertExpectations(t)
}

// TestSend_NoAmountOption_UsesAddressEmbedded verifies that an
// embedded-amount address (V0/V1, or V2 with a baked-in amount) sends
// correctly when the caller does not pass WithAmount.
func TestSend_NoAmountOption_UsesAddressEmbedded(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	addr := encodeEmbedded(t, 42, entities.AddressVersionV0)

	expectedTransfer := &entities.AssetTransfer{AnchorTxid: "def456"}

	mc.On("SendAsset", ctx, mock.MatchedBy(
		func(req *entities.SendAssetRequest) bool {
			return len(req.Recipients) == 1 &&
				req.Recipients[0].Address == addr &&
				req.Recipients[0].Amount == nil
		}),
	).Return(expectedTransfer, nil)

	transfer, err := w.Send(ctx, addr)
	require.NoError(t, err)
	require.Equal(t, expectedTransfer, transfer)

	mc.AssertExpectations(t)
}

// TestSend_V2_AmountRequired rejects a V2 address with no embedded
// amount when the caller does not specify WithAmount.
func TestSend_V2_AmountRequired(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	addr := encodeV2NoAmount(t)

	_, err := w.Send(ctx, addr)
	require.ErrorIs(t, err, ErrAmountRequired)

	mc.AssertExpectations(t)
}

// TestSend_AmountMismatch rejects an explicit amount that contradicts
// the address-embedded amount.
func TestSend_AmountMismatch(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	addr := encodeEmbedded(t, 100, entities.AddressVersionV1)

	_, err := w.Send(ctx, addr, WithAmount(250))
	require.ErrorIs(t, err, ErrAmountMismatch)

	mc.AssertExpectations(t)
}

// TestSend_AmountMatchesEmbedded accepts an explicit amount that
// matches the embedded value and routes through the explicit
// Recipients path — the caller's intent is to supply the amount, so
// the SDK preserves that on the wire.
func TestSend_AmountMatchesEmbedded(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	addr := encodeEmbedded(t, 100, entities.AddressVersionV1)
	mc.On("SendAsset", ctx, mock.MatchedBy(
		func(req *entities.SendAssetRequest) bool {
			return len(req.Recipients) == 1 &&
				req.Recipients[0].Address == addr &&
				req.Recipients[0].Amount != nil &&
				*req.Recipients[0].Amount == 100
		}),
	).Return(&entities.AssetTransfer{AnchorTxid: "match"}, nil)

	transfer, err := w.Send(ctx, addr, WithAmount(100))
	require.NoError(t, err)
	require.NotNil(t, transfer)

	mc.AssertExpectations(t)
}

// TestSend_DecodeError surfaces an error when the address string is
// not a valid bech32m Tap address.
func TestSend_DecodeError(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	_, err := w.Send(ctx, "not-a-tap-address", WithAmount(10))
	require.Error(t, err)

	mc.AssertExpectations(t)
}

func TestSend_Error(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	addr := encodeV2NoAmount(t)
	mc.On("SendAsset", ctx, mock.Anything).Return(
		nil, context.DeadlineExceeded,
	)

	_, err := w.Send(ctx, addr, WithAmount(100))
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	mc.AssertExpectations(t)
}

func TestSend_WithSkipProofCourierPingCheck(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	addr := encodeV2NoAmount(t)
	expectedTransfer := &entities.AssetTransfer{AnchorTxid: "ghi789"}

	mc.On("SendAsset", ctx, mock.MatchedBy(
		func(req *entities.SendAssetRequest) bool {
			return req.SkipProofCourierPingCheck
		}),
	).Return(expectedTransfer, nil)

	transfer, err := w.Send(
		ctx, addr,
		WithAmount(42),
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

	ref := entities.AssetRefFromGroupKey(testKey(t, 21))
	aliceAddr := encodeV2NoAmountForRef(t, ref, 31)
	bobAddr := encodeV2NoAmountForRef(t, ref, 41)

	recipients := []entities.Recipient{
		{Address: aliceAddr, Amount: amountp(100)},
		{Address: bobAddr, Amount: amountp(200)},
	}

	expectedTransfer := &entities.AssetTransfer{AnchorTxid: "multi123"}

	mc.On("SendAsset", ctx, mock.MatchedBy(
		func(req *entities.SendAssetRequest) bool {
			return len(req.Recipients) == 2 &&
				req.Recipients[0].Address == aliceAddr &&
				req.Recipients[0].Amount != nil &&
				*req.Recipients[0].Amount == 100 &&
				req.Recipients[1].Address == bobAddr &&
				req.Recipients[1].Amount != nil &&
				*req.Recipients[1].Amount == 200
		}),
	).Return(expectedTransfer, nil)

	transfer, err := w.SendMulti(ctx, recipients)
	require.NoError(t, err)
	require.Equal(t, expectedTransfer, transfer)

	mc.AssertExpectations(t)
}

func TestSendMulti_RejectsMixedAssetRefs(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	aliceAddr := encodeV2NoAmount(t, 21)
	bobAddr := encodeV2NoAmount(t, 22)

	recipients := []entities.Recipient{
		{Address: aliceAddr, Amount: amountp(100)},
		{Address: bobAddr, Amount: amountp(200)},
	}

	_, err := w.SendMulti(ctx, recipients)
	require.ErrorIs(t, err, ErrMixedAssetBatchUnsupported)

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

	addr := encodeV2NoAmount(t)
	recipients := []entities.Recipient{
		{Address: addr, Amount: amountp(50)},
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

// TestSendMulti_MixedAmountsNormalised feeds SendMulti a batch where
// one recipient has an explicit amount and another leaves Amount nil.
// The low-level SendAsset must still see a uniform shape, so the SDK
// echoes the embedded value into the nil-Amount slot.
func TestSendMulti_MixedAmountsNormalised(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	ref := entities.AssetRefFromGroupKey(testKey(t, 41))
	explicitAddr := encodeV2NoAmountForRef(t, ref, 51)
	embeddedAddr := encodeV2EmbeddedForRef(t, ref, 75, 61)

	recipients := []entities.Recipient{
		{Address: explicitAddr, Amount: amountp(200)},
		{Address: embeddedAddr}, // Amount nil; embedded is 75
	}

	mc.On("SendAsset", ctx, mock.MatchedBy(
		func(req *entities.SendAssetRequest) bool {
			return len(req.Recipients) == 2 &&
				req.Recipients[0].Address == explicitAddr &&
				req.Recipients[0].Amount != nil &&
				*req.Recipients[0].Amount == 200 &&
				req.Recipients[1].Address == embeddedAddr &&
				req.Recipients[1].Amount != nil &&
				*req.Recipients[1].Amount == 75
		}),
	).Return(&entities.AssetTransfer{AnchorTxid: "mix"}, nil)

	_, err := w.SendMulti(ctx, recipients)
	require.NoError(t, err)

	mc.AssertExpectations(t)
}

// TestSendMulti_AmountMismatch rejects a Recipient whose explicit
// amount disagrees with the address-embedded amount.
func TestSendMulti_AmountMismatch(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	addr := encodeEmbedded(t, 75, entities.AddressVersionV1)
	_, err := w.SendMulti(ctx, []entities.Recipient{
		{Address: addr, Amount: amountp(200)},
	})
	require.ErrorIs(t, err, ErrAmountMismatch)

	mc.AssertExpectations(t)
}

// TestSendMulti_AmountRequired rejects a V2 address with no embedded
// amount when the caller does not specify one.
func TestSendMulti_AmountRequired(t *testing.T) {
	mc := new(mockClient)
	w := NewWallet(mc, entities.NetworkRegtest)
	ctx := context.Background()

	addr := encodeV2NoAmount(t)
	recipients := []entities.Recipient{{Address: addr}}

	_, err := w.SendMulti(ctx, recipients)
	require.ErrorIs(t, err, ErrAmountRequired)

	mc.AssertExpectations(t)
}
