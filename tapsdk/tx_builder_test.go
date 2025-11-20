package tapsdk

import (
	"context"
	"testing"
	"time"

	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/taproot-assets/taprpc/assetwalletrpc"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockWalletKitClient is a mock for WalletKitClient.
type MockWalletKitClient struct {
	mock.Mock
}

func (m *MockWalletKitClient) RawClientWithMacAuth(
	parentCtx context.Context) (context.Context, time.Duration,
	assetwalletrpc.AssetWalletClient) {

	args := m.Called(parentCtx)
	return args.Get(0).(context.Context), args.Get(1).(time.Duration),
		args.Get(2).(assetwalletrpc.AssetWalletClient)
}

func (m *MockWalletKitClient) FundTransfer(ctx context.Context,
	recipients []entities.Recipient, inputs []entities.AssetInput) (
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

func TestTxBuilder_Execute(t *testing.T) {
	mockWalletKit := new(MockWalletKitClient)
	services := &Wallet{
		WalletKit: mockWalletKit,
	}

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
				recipients[0].Amount == amount
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
	builder := NewTxBuilder(services)
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
	require.ErrorContains(t, err, "builder already finished")

	mockWalletKit.AssertExpectations(t)
}

func TestTxBuilder_StateInjection(t *testing.T) {
	mockWalletKit := new(MockWalletKitClient)
	services := &Wallet{
		WalletKit: mockWalletKit,
	}

	ctx := context.Background()
	fundedPsbt := []byte("funded_psbt")
	signedPsbt := []byte("signed_psbt")
	anchorPsbt := []byte("anchor_psbt")
	finalAnchorTx := []byte("final_anchor_tx")
	passivePsbts := [][]byte{[]byte("passive_psbt")}

	// Inject externally produced PSBTs to skip earlier steps.
	builder := NewTxBuilder(services)
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
