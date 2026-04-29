package tapsdk

import (
	"context"
	"testing"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockWalletKitClient is a mock for WalletKitClient.
type MockWalletKitClient struct {
	mock.Mock
}

func (m *MockWalletKitClient) FundTransfer(ctx context.Context,
	recipients []entities.Recipient, inputs []entities.PrevID) (
	*entities.FundedTransfer, error) {

	args := m.Called(ctx, recipients, inputs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.FundedTransfer), args.Error(1)
}

func (m *MockWalletKitClient) SignVirtualPsbt(ctx context.Context,
	fundedPsbt []byte) ([]byte, error) {

	args := m.Called(ctx, fundedPsbt)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockWalletKitClient) CommitVirtualPsbts(ctx context.Context,
	virtualPsbts [][]byte, passivePsbts [][]byte,
	satPerVByte uint64) (*entities.CommittedTransfer, error) {

	args := m.Called(ctx, virtualPsbts, passivePsbts, satPerVByte)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.CommittedTransfer), args.Error(1)
}

func (m *MockWalletKitClient) PublishAndLogTransfer(ctx context.Context,
	anchorPsbt []byte, virtualPsbts [][]byte, passivePsbts [][]byte,
	skipAnchorTxBroadcast bool) (*entities.AssetPacket, error) {

	args := m.Called(ctx, anchorPsbt, virtualPsbts, passivePsbts,
		skipAnchorTxBroadcast)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.AssetPacket), args.Error(1)
}

func (m *MockWalletKitClient) DeriveScriptKey(ctx context.Context) (
	*entities.ScriptKey, error) {

	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.ScriptKey), args.Error(1)
}

func (m *MockWalletKitClient) DeriveInternalKey(ctx context.Context) (
	*entities.InternalKey, error) {

	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.InternalKey), args.Error(1)
}

func (m *MockWalletKitClient) FundInteractivePsbt(ctx context.Context,
	psbt []byte) (*entities.FundedTransfer, error) {

	args := m.Called(ctx, psbt)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.FundedTransfer), args.Error(1)
}

func (m *MockWalletKitClient) AnchorVirtualPsbts(ctx context.Context,
	signedPsbts [][]byte) (*entities.AssetTransfer, error) {

	args := m.Called(ctx, signedPsbts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.AssetTransfer), args.Error(1)
}

func (m *MockWalletKitClient) QueryInternalKey(
	ctx context.Context,
	internalKey []byte) (*entities.KeyDescriptor, error) {

	args := m.Called(ctx, internalKey)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.KeyDescriptor), args.Error(1)
}

func (m *MockWalletKitClient) QueryScriptKey(
	ctx context.Context,
	tweakedScriptKey []byte) (*entities.ScriptKey, error) {

	args := m.Called(ctx, tweakedScriptKey)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.ScriptKey), args.Error(1)
}

func (m *MockWalletKitClient) ProveAssetOwnership(
	ctx context.Context,
	req *entities.ProveOwnershipRequest) (
	*entities.OwnershipProof, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.OwnershipProof),
		args.Error(1)
}

func (m *MockWalletKitClient) VerifyAssetOwnership(
	ctx context.Context,
	req *entities.VerifyOwnershipRequest) (
	*entities.VerifyOwnershipResponse, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.VerifyOwnershipResponse),
		args.Error(1)
}

func (m *MockWalletKitClient) RemoveUTXOLease(
	ctx context.Context,
	outpoint entities.Outpoint) error {

	args := m.Called(ctx, outpoint)

	return args.Error(0)
}

func (m *MockWalletKitClient) DeclareScriptKey(
	ctx context.Context,
	req *entities.DeclareScriptKeyRequest) (
	*entities.ScriptKey, error) {

	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*entities.ScriptKey), args.Error(1)
}

func (m *MockWalletKitClient) ExportBackup(ctx context.Context,
	mode entities.BackupMode) ([]byte, error) {

	args := m.Called(ctx, mode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockWalletKitClient) ImportBackup(ctx context.Context,
	backup []byte) (uint32, error) {

	args := m.Called(ctx, backup)
	return uint32(args.Int(0)), args.Error(1)
}

func TestTxBuilder_Execute(t *testing.T) {
	mockWalletKit := new(MockWalletKitClient)

	// Test data
	ctx := context.Background()
	addr := "tap1xyz"
	amount := uint64(100)
	feeRate := uint64(50)
	fundedPsbt := []byte("funded_psbt")
	signedPsbt := []byte("signed_psbt")
	anchorPsbt := []byte("anchor_psbt")
	finalAnchorTx := []byte("final_anchor_tx")

	// 1. Fund
	mockWalletKit.On("FundTransfer", ctx, mock.MatchedBy(
		func(recipients []entities.Recipient) bool {
			return len(recipients) == 1 &&
				recipients[0].Address == addr &&
				recipients[0].Amount != nil &&
				*recipients[0].Amount == amount
		}), mock.Anything).Return(&entities.FundedTransfer{
		FundedPsbt: fundedPsbt,
	}, nil)

	// 2. Sign
	mockWalletKit.On("SignVirtualPsbt", ctx, fundedPsbt).Return(
		signedPsbt, nil)

	// 3. Commit
	mockWalletKit.On("CommitVirtualPsbts", ctx, [][]byte{signedPsbt},
		mock.Anything, feeRate).Return(
		&entities.CommittedTransfer{
			AnchorPsbt:   anchorPsbt,
			VirtualPsbts: [][]byte{signedPsbt},
		}, nil)

	// 4. Finish
	expectedPacket := &entities.AssetPacket{
		AnchorTransaction:   finalAnchorTx,
		VirtualTransactions: [][]byte{signedPsbt},
	}
	mockWalletKit.On("PublishAndLogTransfer", ctx, anchorPsbt,
		[][]byte{signedPsbt}, mock.Anything, false).Return(
		expectedPacket, nil)

	// Execute all steps manually.
	builder := newTxBuilder(mockWalletKit)
	builder.AddRecipient(addr, amount).SetFeeRate(feeRate)

	funded, err := builder.Fund(ctx)
	require.NoError(t, err)
	require.Equal(t, fundedPsbt, funded.FundedPsbt)

	signed, err := builder.Sign(ctx)
	require.NoError(t, err)
	require.Equal(t, signedPsbt, signed)

	committed, err := builder.Commit(ctx)
	require.NoError(t, err)
	require.Equal(t, anchorPsbt, committed.AnchorPsbt)
	require.Equal(t, [][]byte{signedPsbt}, committed.VirtualPsbts)

	packet, err := builder.Finish(ctx, false)
	require.NoError(t, err)

	require.Equal(t, finalAnchorTx, packet.AnchorTransaction)
	require.Equal(t, [][]byte{signedPsbt}, packet.VirtualTransactions)

	// Verify re-calling Finish fails
	_, err = builder.Finish(ctx, false)
	require.ErrorIs(t, err, ErrBuilderFinished)

	mockWalletKit.AssertExpectations(t)
}

func TestTxBuilder_StateInjection(t *testing.T) {
	mockWalletKit := new(MockWalletKitClient)

	ctx := context.Background()
	fundedPsbt := []byte("funded_psbt")
	signedPsbt := []byte("signed_psbt")
	anchorPsbt := []byte("anchor_psbt")
	finalAnchorTx := []byte("final_anchor_tx")
	passivePsbts := [][]byte{[]byte("passive_psbt")}

	// Inject externally produced PSBTs to skip earlier steps.
	builder := newTxBuilder(mockWalletKit)
	builder.SetFundedPsbt(fundedPsbt).
		SetSignedPsbt(signedPsbt).
		SetPassivePsbts(passivePsbts)

	mockWalletKit.On("CommitVirtualPsbts", ctx, [][]byte{signedPsbt},
		passivePsbts, uint64(1)).Return(
		&entities.CommittedTransfer{
			AnchorPsbt:        anchorPsbt,
			VirtualPsbts:      [][]byte{signedPsbt},
			PassiveAssetPsbts: passivePsbts,
		}, nil)

	expectedPacket := &entities.AssetPacket{
		AnchorTransaction:   finalAnchorTx,
		VirtualTransactions: [][]byte{signedPsbt},
	}
	mockWalletKit.On("PublishAndLogTransfer", ctx, anchorPsbt,
		[][]byte{signedPsbt}, mock.Anything, false).Return(
		expectedPacket, nil)

	_, err := builder.Commit(ctx)
	require.NoError(t, err)

	packet, err := builder.Finish(ctx, false)
	require.NoError(t, err)
	require.Equal(t, expectedPacket, packet)

	mockWalletKit.AssertExpectations(t)
}

func TestTxBuilder_AnchorSigning(t *testing.T) {
	mockWalletKit := new(MockWalletKitClient)

	ctx := context.Background()
	signedPsbt := []byte("signed_psbt")
	anchorPsbt := []byte("anchor_psbt")
	signedAnchorPsbt := []byte("signed_anchor_psbt")
	finalAnchorTx := []byte("final_anchor_tx")

	builder := newTxBuilder(mockWalletKit)
	builder.SetSignedPsbt(signedPsbt).SetAnchorSigner(
		func(_ context.Context, psbt []byte) ([]byte, error) {
			require.Equal(t, anchorPsbt, psbt)
			return signedAnchorPsbt, nil
		},
	)

	mockWalletKit.On("CommitVirtualPsbts", ctx, [][]byte{signedPsbt},
		mock.Anything, uint64(1)).Return(
		&entities.CommittedTransfer{
			AnchorPsbt:   anchorPsbt,
			VirtualPsbts: [][]byte{signedPsbt},
		}, nil)

	expectedPacket := &entities.AssetPacket{
		AnchorTransaction:   finalAnchorTx,
		VirtualTransactions: [][]byte{signedPsbt},
	}
	mockWalletKit.On("PublishAndLogTransfer", ctx, signedAnchorPsbt,
		[][]byte{signedPsbt}, mock.Anything, false).Return(
		expectedPacket, nil)

	_, err := builder.Commit(ctx)
	require.NoError(t, err)

	packet, err := builder.Finish(ctx, false)
	require.NoError(t, err)
	require.Equal(t, expectedPacket, packet)

	mockWalletKit.AssertExpectations(t)
}

func TestTxBuilder_AnchorPsbtInjection(t *testing.T) {
	mockWalletKit := new(MockWalletKitClient)

	ctx := context.Background()
	signedPsbt := []byte("signed_psbt")
	anchorPsbt := []byte("signed_anchor_psbt")
	finalAnchorTx := []byte("final_anchor_tx")

	builder := newTxBuilder(mockWalletKit)
	builder.SetSignedPsbt(signedPsbt).SetAnchorPsbt(anchorPsbt)

	expectedPacket := &entities.AssetPacket{
		AnchorTransaction:   finalAnchorTx,
		VirtualTransactions: [][]byte{signedPsbt},
	}
	mockWalletKit.On("PublishAndLogTransfer", ctx, anchorPsbt,
		[][]byte{signedPsbt}, mock.Anything, true).Return(
		expectedPacket, nil)

	packet, err := builder.Finish(ctx, true)
	require.NoError(t, err)
	require.Equal(t, expectedPacket, packet)

	mockWalletKit.AssertExpectations(t)
}

func TestTxBuilder_NoRecipients(t *testing.T) {
	mockWalletKit := new(MockWalletKitClient)

	ctx := context.Background()
	builder := newTxBuilder(mockWalletKit)

	// Fund without setting recipients should fail.
	_, err := builder.Fund(ctx)
	require.ErrorIs(t, err, ErrNoRecipients)

	// Execute without setting recipients should fail.
	_, err = builder.Execute(ctx, false)
	require.ErrorIs(t, err, ErrNoRecipients)
}
