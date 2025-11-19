package tapsdk

import (
	"context"
	"testing"
	"time"

	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/lightninglabs/taproot-assets/taprpc/assetwalletrpc"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
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

// MockAssetWalletClient is a mock for assetwalletrpc.AssetWalletClient.
type MockAssetWalletClient struct {
	mock.Mock
}

func (m *MockAssetWalletClient) FundVirtualPsbt(ctx context.Context,
	in *assetwalletrpc.FundVirtualPsbtRequest,
	opts ...grpc.CallOption) (*assetwalletrpc.FundVirtualPsbtResponse,
	error) {

	args := m.Called(ctx, in, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*assetwalletrpc.FundVirtualPsbtResponse), args.Error(1)
}

func (m *MockAssetWalletClient) SignVirtualPsbt(ctx context.Context,
	in *assetwalletrpc.SignVirtualPsbtRequest,
	opts ...grpc.CallOption) (*assetwalletrpc.SignVirtualPsbtResponse,
	error) {

	args := m.Called(ctx, in, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*assetwalletrpc.SignVirtualPsbtResponse), args.Error(1)
}

func (m *MockAssetWalletClient) CommitVirtualPsbts(ctx context.Context,
	in *assetwalletrpc.CommitVirtualPsbtsRequest,
	opts ...grpc.CallOption) (*assetwalletrpc.CommitVirtualPsbtsResponse,
	error) {

	args := m.Called(ctx, in, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*assetwalletrpc.CommitVirtualPsbtsResponse),
		args.Error(1)
}

func (m *MockAssetWalletClient) PublishAndLogTransfer(ctx context.Context,
	in *assetwalletrpc.PublishAndLogRequest,
	opts ...grpc.CallOption) (*taprpc.SendAssetResponse, error) {

	args := m.Called(ctx, in, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*taprpc.SendAssetResponse), args.Error(1)
}

// Stubs for other methods of AssetWalletClient.
func (m *MockAssetWalletClient) AnchorVirtualPsbts(ctx context.Context,
	in *assetwalletrpc.AnchorVirtualPsbtsRequest,
	opts ...grpc.CallOption) (*taprpc.SendAssetResponse, error) {

	return nil, nil
}

func (m *MockAssetWalletClient) NextInternalKey(ctx context.Context,
	in *assetwalletrpc.NextInternalKeyRequest,
	opts ...grpc.CallOption) (*assetwalletrpc.NextInternalKeyResponse,
	error) {

	return nil, nil
}

func (m *MockAssetWalletClient) NextScriptKey(ctx context.Context,
	in *assetwalletrpc.NextScriptKeyRequest,
	opts ...grpc.CallOption) (*assetwalletrpc.NextScriptKeyResponse, error) {

	return nil, nil
}

func (m *MockAssetWalletClient) QueryInternalKey(ctx context.Context,
	in *assetwalletrpc.QueryInternalKeyRequest,
	opts ...grpc.CallOption) (*assetwalletrpc.QueryInternalKeyResponse,
	error) {

	return nil, nil
}

func (m *MockAssetWalletClient) QueryScriptKey(ctx context.Context,
	in *assetwalletrpc.QueryScriptKeyRequest,
	opts ...grpc.CallOption) (*assetwalletrpc.QueryScriptKeyResponse,
	error) {

	return nil, nil
}

func (m *MockAssetWalletClient) ProveAssetOwnership(ctx context.Context,
	in *assetwalletrpc.ProveAssetOwnershipRequest,
	opts ...grpc.CallOption) (*assetwalletrpc.ProveAssetOwnershipResponse,
	error) {

	return nil, nil
}

func (m *MockAssetWalletClient) VerifyAssetOwnership(ctx context.Context,
	in *assetwalletrpc.VerifyAssetOwnershipRequest,
	opts ...grpc.CallOption) (*assetwalletrpc.VerifyAssetOwnershipResponse,
	error) {

	return nil, nil
}

func (m *MockAssetWalletClient) RemoveUTXOLease(ctx context.Context,
	in *assetwalletrpc.RemoveUTXOLeaseRequest,
	opts ...grpc.CallOption) (*assetwalletrpc.RemoveUTXOLeaseResponse,
	error) {

	return nil, nil
}

func (m *MockAssetWalletClient) DeclareScriptKey(ctx context.Context,
	in *assetwalletrpc.DeclareScriptKeyRequest,
	opts ...grpc.CallOption) (*assetwalletrpc.DeclareScriptKeyResponse,
	error) {

	return nil, nil
}

func TestTxBuilder_Finish(t *testing.T) {
	mockWalletKit := new(MockWalletKitClient)
	mockAssetWallet := new(MockAssetWalletClient)
	services := &Wallet{
		WalletKit: mockWalletKit,
	}

	// Setup expectation: RawClientWithMacAuth should be called to get the
	// client.
	mockWalletKit.On("RawClientWithMacAuth", mock.Anything).Return(
		context.Background(), time.Duration(0), mockAssetWallet,
	)

	// Test data
	ctx := context.Background()
	addr := "tap1xyz"
	amount := uint64(100)
	feeRate := uint64(50)
	fundedPsbt := []byte("funded_psbt")
	signedPsbt := []byte("signed_psbt")
	anchorPsbt := []byte("anchor_psbt")
	finalAnchorTx := []byte("final_anchor_tx")

	// Response from PublishAndLogTransfer
	sendResp := &taprpc.SendAssetResponse{
		Transfer: &taprpc.AssetTransfer{
			AnchorTx: finalAnchorTx,
		},
	}

	// 1. Fund
	mockAssetWallet.On("FundVirtualPsbt", ctx, mock.MatchedBy(
		func(req *assetwalletrpc.FundVirtualPsbtRequest) bool {
			raw := req.GetRaw()
			return raw != nil &&
				raw.AddressesWithAmounts[0].TapAddr == addr &&
				raw.AddressesWithAmounts[0].Amount == amount
		}), mock.Anything).Return(&assetwalletrpc.FundVirtualPsbtResponse{
		FundedPsbt: fundedPsbt,
	}, nil)

	// 2. Sign
	mockAssetWallet.On("SignVirtualPsbt", ctx, mock.MatchedBy(
		func(req *assetwalletrpc.SignVirtualPsbtRequest) bool {
			return string(req.FundedPsbt) == string(fundedPsbt)
		}), mock.Anything).Return(&assetwalletrpc.SignVirtualPsbtResponse{
		SignedPsbt: signedPsbt,
	}, nil)

	// 3. Commit
	mockAssetWallet.On("CommitVirtualPsbts", ctx, mock.MatchedBy(
		func(req *assetwalletrpc.CommitVirtualPsbtsRequest) bool {
			return string(req.VirtualPsbts[0]) == string(signedPsbt) &&
				req.GetSatPerVbyte() == feeRate
		}), mock.Anything).Return(
		&assetwalletrpc.CommitVirtualPsbtsResponse{
			AnchorPsbt:   anchorPsbt,
			VirtualPsbts: [][]byte{signedPsbt},
		}, nil)

	// 4. Finish
	mockAssetWallet.On("PublishAndLogTransfer", ctx, mock.MatchedBy(
		func(req *assetwalletrpc.PublishAndLogRequest) bool {
			return string(req.AnchorPsbt) == string(anchorPsbt) &&
				string(req.VirtualPsbts[0]) == string(signedPsbt) &&
				!req.SkipAnchorTxBroadcast
		}), mock.Anything).Return(sendResp, nil)

	// Execute all steps manually.
	builder := NewTxBuilder(services)
	builder.AddRecipient(addr, amount).SetFeeRate(feeRate)

	err := builder.Fund(ctx)
	require.NoError(t, err)

	err = builder.Sign(ctx)
	require.NoError(t, err)

	err = builder.Commit(ctx)
	require.NoError(t, err)

	packet, err := builder.Finish(ctx, false)
	require.NoError(t, err)

	require.Equal(t, finalAnchorTx, packet.AnchorTransaction)
	require.Equal(t, [][]byte{signedPsbt}, packet.VirtualTransactions)

	// Verify re-calling Finish fails
	_, err = builder.Finish(ctx, false)
	require.ErrorContains(t, err, "builder already finished")

	mockWalletKit.AssertExpectations(t)
	mockAssetWallet.AssertExpectations(t)
}
