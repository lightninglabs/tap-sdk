package customanchor

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/commitment"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightninglabs/taproot-assets/tappsbt"
	"github.com/lightninglabs/taproot-assets/tapscript"
	"github.com/lightninglabs/taproot-assets/tapsend"
	"github.com/lightninglabs/taproot-assets/vm"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCustomAnchorCommitCapabilitiesFailClosed(t *testing.T) {
	t.Parallel()

	fixture := newCustomAnchorBuilderFixture(t)
	plan, err := newTestTxBuilder(fixture.client).Build(
		context.Background(), fixture.request,
	)
	require.NoError(t, err)

	_, err = plan.Commit(context.Background())
	require.Error(t, err)
	var capabilityErr *UnsupportedCapabilityError
	require.ErrorAs(t, err, &capabilityErr)
	require.Equal(t, CapabilityCommit, capabilityErr.Capability)
	require.Equal(t, CapabilityUnknown, capabilityErr.Status)
}

func TestCustomAnchorP2ACommitRequiresSkipBroadcastCapability(t *testing.T) {
	t.Parallel()

	fixture := newCustomAnchorBuilderFixture(t)
	var signCalls, commitCalls int
	fixture.client.signVirtual = func(_ context.Context, packet []byte) (
		[]byte, error) {

		signCalls++
		return packet, nil
	}
	fixture.client.commit = func(context.Context,
		*tapsdk.CommitVirtualPsbtsRequest) (
		*tapsdk.CommitVirtualPsbtsResponse, error) {

		commitCalls++
		return nil, errors.New("unexpected commit")
	}

	capabilities := DefaultTapdCapabilities()
	capabilities.SkipBroadcast = CapabilityUnknown
	client := &capableCustomAnchorBuilderTestClient{
		customAnchorBuilderTestClient: fixture.client,
		capabilities:                  &capabilities,
	}
	plan, err := newTestTxBuilder(client).Build(
		context.Background(), fixture.request,
	)
	require.NoError(t, err)

	_, err = plan.Commit(context.Background())
	require.Error(t, err)
	var capabilityErr *UnsupportedCapabilityError
	require.ErrorAs(t, err, &capabilityErr)
	require.Equal(t, CapabilitySkipBroadcast,
		capabilityErr.Capability)
	require.Zero(t, signCalls)
	require.Zero(t, commitCalls)
}

func TestCustomAnchorLifecycleBackendSigningAndPublish(t *testing.T) {
	t.Parallel()

	fixture := newCustomAnchorBuilderFixture(t)
	policyLeaf := txscript.NewBaseTapLeaf([]byte{txscript.OP_TRUE})
	fixture.request.Outputs[0].Anchor.Tapscript = TapscriptPlan{
		TapLeaves: []tapsdk.TapLeaf{{Script: cloneBytes(policyLeaf.Script)}},
	}
	var (
		signCalls     int
		commitRequest *tapsdk.CommitVirtualPsbtsRequest
		publishReq    *tapsdk.PublishAndLogTransferRequest
	)
	fixture.client.signVirtual = func(_ context.Context, packet []byte) (
		[]byte, error) {

		signCalls++
		return customAnchorSignVirtualPacket(
			t, fixture.assetSpendKey, packet,
		), nil
	}
	fixture.client.commit = func(_ context.Context,
		req *tapsdk.CommitVirtualPsbtsRequest) (
		*tapsdk.CommitVirtualPsbtsResponse, error) {

		commitRequest = cloneCustomAnchorCommitRequest(req)
		return customAnchorTestCommitResponse(t, req), nil
	}
	var expectedResult *tapsdk.AssetPacket
	fixture.client.publish = func(_ context.Context,
		req *tapsdk.PublishAndLogTransferRequest) (*tapsdk.AssetPacket, error) {

		publishReq = cloneCustomAnchorPublishRequest(req)
		packet, err := decodeAnchorPSBT(req.AnchorPsbt)
		require.NoError(t, err)
		finalTx, err := psbt.Extract(packet)
		require.NoError(t, err)
		var anchorTx bytes.Buffer
		require.NoError(t, finalTx.Serialize(&anchorTx))
		expectedResult = &tapsdk.AssetPacket{
			AnchorTransaction: anchorTx.Bytes(),
			VirtualTransactions: cloneByteSlices(
				req.VirtualPsbts,
			),
			PassiveAssetTransactions: cloneByteSlices(
				req.PassiveAssetPsbts,
			),
		}
		return expectedResult, nil
	}

	capabilities := DefaultTapdCapabilities()
	client := &capableCustomAnchorBuilderTestClient{
		customAnchorBuilderTestClient: fixture.client,
		capabilities:                  &capabilities,
	}
	wallet := tapsdk.NewWallet(client, tapsdk.NetworkRegtest)
	plan, err := NewTxBuilder(wallet).Build(
		context.Background(), fixture.request,
	)
	require.NoError(t, err)
	planID, err := plan.ID()
	require.NoError(t, err)

	publishMetadata := PublishMetadata{
		SkipAnchorTxBroadcast: true,
		Label:                 "swapdk-round-42",
		ExternalBroadcast:     true,
	}
	packageSnapshot, err := plan.Commit(
		context.Background(),
		CommitOptions{Publish: publishMetadata},
	)
	require.NoError(t, err)
	require.NoError(t, packageSnapshot.Validate())
	require.True(t, packageSnapshot.Publish.SkipAnchorTxBroadcast)
	require.True(t, packageSnapshot.Publish.ExternalBroadcast)
	require.Equal(t, 1, signCalls)
	require.NotNil(t, commitRequest)
	require.True(t, commitRequest.Funding.SkipFunding)
	require.Equal(t, plan.AnchorPSBT(), commitRequest.AnchorPsbt)
	require.Len(t, commitRequest.VirtualPsbts, 1)
	require.NotEqual(t, plan.ActiveVirtualPSBTs()[0],
		commitRequest.VirtualPsbts[0])
	require.Nil(t, commitRequest.PassiveAssetPsbts)

	require.Equal(t, planID, packageSnapshot.PlanID)
	require.Equal(t, int32(-1), packageSnapshot.ChangeOutputIndex)
	require.Empty(t, packageSnapshot.LockedUTXOs)
	require.Len(t, packageSnapshot.Inputs, 1)
	require.Len(t, packageSnapshot.Outputs, 1)
	require.Len(t, packageSnapshot.ProofUpdates, 1)
	require.NotEmpty(t, packageSnapshot.ProofUpdates[0].ProofBlob)
	transition, err := proof.Decode(
		packageSnapshot.ProofUpdates[0].ProofBlob,
	)
	require.NoError(t, err)
	require.Equal(t, proof.TransitionV1, transition.Version)
	require.Equal(t, publishMetadata, packageSnapshot.Publish)
	outputSummary := packageSnapshot.Outputs[0]
	require.NotZero(t, outputSummary.TaprootAssetRoot)
	require.NotZero(t, outputSummary.TaprootMerkleRoot)
	policyRoot := policyLeaf.TapHash()
	expectedRoot := asset.NewTapBranchHash(
		chainhash.Hash(outputSummary.TaprootAssetRoot), policyRoot,
	)
	require.Equal(t, tapsdk.Hash(expectedRoot), outputSummary.TaprootMerkleRoot)

	before, err := decodeAnchorPSBT(plan.AnchorPSBT())
	require.NoError(t, err)
	committed, err := decodeAnchorPSBT(packageSnapshot.AnchorPsbt)
	require.NoError(t, err)
	require.Equal(t, before.UnsignedTx.Version,
		committed.UnsignedTx.Version)
	require.Equal(t, before.UnsignedTx.LockTime,
		committed.UnsignedTx.LockTime)
	require.Equal(t, before.UnsignedTx.TxIn[0].Sequence,
		committed.UnsignedTx.TxIn[0].Sequence)
	require.Equal(t, before.UnsignedTx.TxIn[1].Sequence,
		committed.UnsignedTx.TxIn[1].Sequence)
	require.Equal(t, before.UnsignedTx.TxOut[0],
		committed.UnsignedTx.TxOut[0])
	require.Equal(t, before.UnsignedTx.TxOut[2],
		committed.UnsignedTx.TxOut[2])
	require.NotEqual(t, before.UnsignedTx.TxOut[1].PkScript,
		committed.UnsignedTx.TxOut[1].PkScript)
	require.Equal(t, before.UnsignedTx.TxOut[1].Value,
		committed.UnsignedTx.TxOut[1].Value)
	internalKey, err := btcec.ParsePubKey(
		fixture.request.Outputs[0].Anchor.InternalKey.PubKey[:],
	)
	require.NoError(t, err)
	outputKey := txscript.ComputeTaprootOutputKey(
		internalKey, outputSummary.TaprootMerkleRoot[:],
	)
	expectedPkScript, err := txscript.PayToTaprootScript(outputKey)
	require.NoError(t, err)
	require.Equal(t, expectedPkScript,
		committed.UnsignedTx.TxOut[outputSummary.AnchorOutputIndex].PkScript)

	_, err = PublishTransfer(
		context.Background(), wallet, packageSnapshot,
		packageSnapshot.AnchorPsbt,
	)
	require.ErrorContains(t, err, "final anchor PSBT is not finalized")
	require.Nil(t, publishReq)

	finalAnchorPSBT := customAnchorFinalizePSBT(
		t, packageSnapshot.AnchorPsbt, fixture.btcInputKey,
		fixture.anchorInputKey,
	)
	result, err := PublishTransfer(
		context.Background(), wallet, packageSnapshot, finalAnchorPSBT,
	)
	require.NoError(t, err)
	require.Same(t, expectedResult, result)
	require.NotNil(t, publishReq)
	require.Equal(t, finalAnchorPSBT, publishReq.AnchorPsbt)
	require.Equal(t, packageSnapshot.ActiveVirtualPsbts,
		publishReq.VirtualPsbts)
	require.Equal(t, packageSnapshot.PassiveVirtualPsbts,
		publishReq.PassiveAssetPsbts)
	require.Equal(t, packageSnapshot.ChangeOutputIndex,
		publishReq.ChangeOutputIndex)
	require.Empty(t, publishReq.LockedUTXOs)
	require.True(t, publishReq.SkipAnchorTxBroadcast)
	require.Equal(t, "swapdk-round-42", publishReq.Label)

	for _, unusable := range []*tapsdk.AssetPacket{nil, {}} {
		fixture.client.publish = func(context.Context,
			*tapsdk.PublishAndLogTransferRequest) (*tapsdk.AssetPacket, error) {

			return unusable, nil
		}
		_, err = PublishTransfer(
			context.Background(), wallet, packageSnapshot, finalAnchorPSBT,
		)
		var attemptErr *PublishAttemptError
		require.ErrorAs(t, err, &attemptErr)
		require.True(t, attemptErr.OutcomeUnknown)
	}

	for _, mismatch := range []*tapsdk.AssetPacket{
		{
			AnchorTransaction:        []byte{1},
			VirtualTransactions:      packageSnapshot.ActiveVirtualPsbts,
			PassiveAssetTransactions: packageSnapshot.PassiveVirtualPsbts,
		},
		{
			AnchorTransaction:        expectedResult.AnchorTransaction,
			VirtualTransactions:      [][]byte{{1}},
			PassiveAssetTransactions: packageSnapshot.PassiveVirtualPsbts,
		},
		{
			AnchorTransaction:   expectedResult.AnchorTransaction,
			VirtualTransactions: packageSnapshot.ActiveVirtualPsbts,
			PassiveAssetTransactions: [][]byte{
				{1},
			},
		},
	} {
		fixture.client.publish = func(context.Context,
			*tapsdk.PublishAndLogTransferRequest) (*tapsdk.AssetPacket, error) {

			return mismatch, nil
		}
		_, err = PublishTransfer(
			context.Background(), wallet, packageSnapshot, finalAnchorPSBT,
		)
		var attemptErr *PublishAttemptError
		require.ErrorAs(t, err, &attemptErr)
		require.True(t, attemptErr.OutcomeUnknown)
		require.ErrorContains(t, err, "does not match")
	}
}

func TestCustomAnchorCommitPersistsFundingLock(t *testing.T) {
	t.Parallel()

	fixture := newCustomAnchorBuilderFixture(t)
	feeRate, err := tapsdk.NewFeeRateSatPerVByte(2)
	require.NoError(t, err)

	lockID := bytes.Repeat([]byte{4}, 32)
	fixture.request.Funding = FundingPlan{
		Mode: FundingWalletFunded,
		WalletFunded: &WalletFunding{
			ChangeOutput: tapsdk.AnchorChangeOutput{
				Mode: tapsdk.AnchorChangeOutputAdd,
			},
			Fee: tapsdk.AnchorFee{
				Mode:    tapsdk.AnchorFeeSatPerVByte,
				FeeRate: feeRate,
			},
			MaxFeeSat:             10_000,
			CustomLockID:          bytes.Clone(lockID),
			LockExpirationSeconds: 900,
		},
	}
	fixture.client.signVirtual = func(_ context.Context, packet []byte) (
		[]byte, error) {

		return customAnchorSignVirtualPacket(
			t, fixture.assetSpendKey, packet,
		), nil
	}
	var commitRequest *tapsdk.CommitVirtualPsbtsRequest
	fixture.client.commit = func(_ context.Context,
		req *tapsdk.CommitVirtualPsbtsRequest) (
		*tapsdk.CommitVirtualPsbtsResponse, error) {

		commitRequest = cloneCustomAnchorCommitRequest(req)
		return customAnchorTestCommitResponse(t, req), nil
	}

	capabilities := DefaultTapdCapabilities()
	client := &capableCustomAnchorBuilderTestClient{
		customAnchorBuilderTestClient: fixture.client,
		capabilities:                  &capabilities,
	}
	plan, err := newTestTxBuilder(client).Build(
		context.Background(), fixture.request,
	)
	require.NoError(t, err)

	fixture.request.Funding.WalletFunded.CustomLockID[0] = 5
	packageSnapshot, err := plan.Commit(context.Background())
	require.NoError(t, err)
	require.NotNil(t, commitRequest)
	require.Equal(t, lockID, commitRequest.Funding.CustomLockID)
	require.Equal(
		t, uint64(900), commitRequest.Funding.LockExpirationSeconds,
	)
	require.Equal(t, lockID, packageSnapshot.FundingLock.CustomLockID)
	require.Equal(
		t, uint64(900),
		packageSnapshot.FundingLock.LockExpirationSeconds,
	)

	clone := packageSnapshot.Clone()
	packageSnapshot.FundingLock.CustomLockID[0] = 6
	require.Equal(t, byte(4), clone.FundingLock.CustomLockID[0])
	clone.FundingLock.CustomLockID[1] = 7
	require.Equal(
		t, byte(4), packageSnapshot.FundingLock.CustomLockID[1],
	)
}

func TestCustomAnchorCommitEnforcesWalletFeeCeiling(t *testing.T) {
	t.Parallel()

	fixture := newCustomAnchorBuilderFixture(t)
	anchor := mustDecodeAnchorPSBT(t, fixture.request.AnchorPSBT)
	anchor.UnsignedTx.TxOut = anchor.UnsignedTx.TxOut[:2]
	anchor.Outputs = anchor.Outputs[:2]
	fixture.request.AnchorPSBT = mustSerializeAnchorPSBT(t, anchor)

	feeRate, err := tapsdk.NewFeeRateSatPerVByte(2)
	require.NoError(t, err)
	lockID := bytes.Repeat([]byte{7}, 32)
	fixture.request.Funding = FundingPlan{
		Mode: FundingWalletFunded,
		WalletFunded: &WalletFunding{
			ChangeOutput: tapsdk.AnchorChangeOutput{
				Mode: tapsdk.AnchorChangeOutputAdd,
			},
			Fee: tapsdk.AnchorFee{
				Mode:    tapsdk.AnchorFeeSatPerVByte,
				FeeRate: feeRate,
			},
			MaxFeeSat:    100,
			CustomLockID: lockID,
		},
	}
	fixture.client.signVirtual = func(_ context.Context, packet []byte) (
		[]byte, error) {

		return customAnchorSignVirtualPacket(
			t, fixture.assetSpendKey, packet,
		), nil
	}
	fixture.client.commit = func(_ context.Context,
		req *tapsdk.CommitVirtualPsbtsRequest) (
		*tapsdk.CommitVirtualPsbtsResponse, error) {

		response := customAnchorTestCommitResponse(t, req)
		committed := mustDecodeAnchorPSBT(t, response.AnchorPsbt)
		backendKey := testPrivateKey(t, 111).PubKey()
		backendScript, err := txscript.PayToTaprootScript(
			txscript.ComputeTaprootKeyNoScript(backendKey),
		)
		require.NoError(t, err)
		backendOutpoint := wire.OutPoint{
			Hash:  chainhash.Hash{111},
			Index: 3,
		}
		committed.UnsignedTx.AddTxIn(&wire.TxIn{
			PreviousOutPoint: backendOutpoint,
		})
		committed.Inputs = append(committed.Inputs, psbt.PInput{
			WitnessUtxo: &wire.TxOut{
				Value:    1_000,
				PkScript: backendScript,
			},
		})
		committed.UnsignedTx.AddTxOut(&wire.TxOut{
			Value:    899,
			PkScript: []byte{txscript.OP_TRUE},
		})
		committed.Outputs = append(committed.Outputs, psbt.POutput{})
		response.AnchorPsbt = mustSerializeAnchorPSBT(t, committed)
		response.ChangeOutputIndex = 2
		response.LockedUTXOs = []tapsdk.Outpoint{
			outpointFromWire(backendOutpoint),
		}

		return response, nil
	}

	capabilities := DefaultTapdCapabilities()
	client := &capableCustomAnchorBuilderTestClient{
		customAnchorBuilderTestClient: fixture.client,
		capabilities:                  &capabilities,
	}
	plan, err := newTestTxBuilder(client).Build(
		context.Background(), fixture.request,
	)
	require.NoError(t, err)

	_, err = plan.Commit(context.Background())
	require.ErrorContains(t, err, "committed anchor fee 101 exceeds maximum 100")
}

func TestCustomAnchorCommitAttemptOutcome(t *testing.T) {
	t.Parallel()

	fixture := newCustomAnchorBuilderFixture(t)
	feeRate, err := tapsdk.NewFeeRateSatPerVByte(2)
	require.NoError(t, err)
	lockID := bytes.Repeat([]byte{8}, 32)
	fixture.request.Funding = FundingPlan{
		Mode: FundingWalletFunded,
		WalletFunded: &WalletFunding{
			ChangeOutput: tapsdk.AnchorChangeOutput{
				Mode: tapsdk.AnchorChangeOutputAdd,
			},
			Fee: tapsdk.AnchorFee{
				Mode:    tapsdk.AnchorFeeSatPerVByte,
				FeeRate: feeRate,
			},
			MaxFeeSat:             10_000,
			CustomLockID:          bytes.Clone(lockID),
			LockExpirationSeconds: 1_200,
		},
	}
	fixture.client.signVirtual = func(_ context.Context, packet []byte) (
		[]byte, error) {

		return customAnchorSignVirtualPacket(
			t, fixture.assetSpendKey, packet,
		), nil
	}

	capabilities := DefaultTapdCapabilities()
	client := &capableCustomAnchorBuilderTestClient{
		customAnchorBuilderTestClient: fixture.client,
		capabilities:                  &capabilities,
	}
	plan, err := newTestTxBuilder(client).Build(
		context.Background(), fixture.request,
	)
	require.NoError(t, err)

	fixture.client.commit = func(_ context.Context,
		req *tapsdk.CommitVirtualPsbtsRequest) (
		*tapsdk.CommitVirtualPsbtsResponse, error) {

		// Model the backend completing commit and retaining its leases,
		// followed by loss of the response at the transport boundary.
		_ = customAnchorTestCommitResponse(t, req)
		return nil, status.Error(
			codes.DeadlineExceeded, "commit response was lost",
		)
	}

	_, err = plan.Commit(context.Background())
	require.Error(t, err)
	var attemptErr *CommitAttemptError
	require.ErrorAs(t, err, &attemptErr)
	require.True(t, attemptErr.OutcomeUnknown)
	require.Equal(t, lockID, attemptErr.FundingLock.CustomLockID)
	require.Equal(
		t, uint64(1_200),
		attemptErr.FundingLock.LockExpirationSeconds,
	)
	attemptErr.FundingLock.CustomLockID[0]++
	require.Equal(t, byte(8), lockID[0])

	fixture.client.commit = func(_ context.Context,
		_ *tapsdk.CommitVirtualPsbtsRequest) (
		*tapsdk.CommitVirtualPsbtsResponse, error) {

		return nil, status.Error(
			codes.InvalidArgument, "request was rejected",
		)
	}
	_, err = plan.Commit(context.Background())
	require.ErrorAs(t, err, &attemptErr)
	require.False(t, attemptErr.OutcomeUnknown)

	fixture.client.commit = func(_ context.Context,
		_ *tapsdk.CommitVirtualPsbtsRequest) (
		*tapsdk.CommitVirtualPsbtsResponse, error) {

		return nil, nil
	}
	_, err = plan.Commit(context.Background())
	require.ErrorAs(t, err, &attemptErr)
	require.True(t, attemptErr.OutcomeUnknown)
	require.Equal(t, lockID, attemptErr.FundingLock.CustomLockID)
}

func TestCustomAnchorPublishOutcomeClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		unknown bool
	}{
		{
			name:    "lost response",
			err:     status.Error(codes.DeadlineExceeded, "lost"),
			unknown: true,
		},
		{
			name:    "duplicate may be prior success",
			err:     status.Error(codes.AlreadyExists, "duplicate"),
			unknown: true,
		},
		{
			name: "explicit rejection",
			err:  status.Error(codes.InvalidArgument, "rejected"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(
				t, test.unknown,
				customAnchorPublishOutcomeUnknown(test.err),
			)
		})
	}
}

func TestCustomAnchorCommitOutcomeClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		unknown bool
	}{
		{
			name:    "lost response",
			err:     status.Error(codes.DeadlineExceeded, "lost"),
			unknown: true,
		},
		{
			name:    "duplicate may be prior success",
			err:     status.Error(codes.AlreadyExists, "duplicate"),
			unknown: true,
		},
		{
			name: "explicit rejection",
			err:  status.Error(codes.InvalidArgument, "rejected"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(
				t, test.unknown,
				customAnchorCommitOutcomeUnknown(test.err),
			)
		})
	}
}

func TestValidateCustomAnchorLockInventory(t *testing.T) {
	t.Parallel()

	fixture := newCustomAnchorBuilderFixture(t)
	anchor, err := decodeAnchorPSBT(fixture.request.AnchorPSBT)
	require.NoError(t, err)

	var backendHash chainhash.Hash
	backendHash[0] = 9
	backendOutpoint := wire.OutPoint{
		Hash:  backendHash,
		Index: 3,
	}
	anchor.UnsignedTx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: backendOutpoint,
	})
	anchor.Inputs = append(anchor.Inputs, psbt.PInput{})
	locked := LockedUTXO{
		Outpoint: outpointFromWire(backendOutpoint),
	}
	wrong := LockedUTXO{
		Outpoint: tapsdk.Outpoint{Txid: [32]byte{8}, Index: 4},
	}
	callerFunding := FundingPlan{
		Mode:              FundingCallerFundedExact,
		CallerFundedExact: &CallerFundedExact{},
	}
	walletFunding := FundingPlan{
		Mode:         FundingWalletFunded,
		WalletFunded: &WalletFunding{},
	}

	tests := []struct {
		name           string
		funding        FundingPlan
		backendManaged []uint32
		locked         []LockedUTXO
		wantErr        string
	}{
		{
			name:    "skip funding without locks",
			funding: callerFunding,
		},
		{
			name:    "skip funding with lock",
			funding: callerFunding,
			locked:  []LockedUTXO{locked},
			wantErr: "skip-funding commit returned locked UTXOs",
		},
		{
			name:           "wallet lock covers backend input",
			funding:        walletFunding,
			backendManaged: []uint32{2},
			locked:         []LockedUTXO{locked},
		},
		{
			name:           "missing wallet lock",
			funding:        walletFunding,
			backendManaged: []uint32{2},
			wantErr:        "do not cover backend-funded inputs",
		},
		{
			name:           "wrong wallet lock",
			funding:        walletFunding,
			backendManaged: []uint32{2},
			locked:         []LockedUTXO{wrong},
			wantErr:        "do not cover backend-funded inputs",
		},
		{
			name:           "duplicate wallet lock",
			funding:        walletFunding,
			backendManaged: []uint32{2},
			locked:         []LockedUTXO{locked, locked},
			wantErr:        "locked UTXO 1 is duplicated",
		},
		{
			name:           "backend input out of range",
			funding:        walletFunding,
			backendManaged: []uint32{3},
			wantErr:        "backend-managed input 3 is out of range",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateCustomAnchorLockInventory(
				test.funding, anchor, test.backendManaged,
				test.locked,
			)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestCustomAnchorCommitRejectsBackendResponseDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		mutateResponse func(*tapsdk.CommitVirtualPsbtsResponse)
		mutatePackets  func(*psbt.Packet, []*tappsbt.VPacket)
		wantErr        string
	}{
		{
			name: "version",
			mutateResponse: func(response *tapsdk.CommitVirtualPsbtsResponse) {
				customAnchorMutateResponsePSBT(
					t, response,
					func(packet *psbt.Packet) {
						packet.UnsignedTx.Version++
					},
				)
			},
			wantErr: "changed anchor version or locktime",
		},
		{
			name: "locktime",
			mutateResponse: func(response *tapsdk.CommitVirtualPsbtsResponse) {
				customAnchorMutateResponsePSBT(
					t, response,
					func(packet *psbt.Packet) {
						packet.UnsignedTx.LockTime++
					},
				)
			},
			wantErr: "changed anchor version or locktime",
		},
		{
			name: "input sequence",
			mutateResponse: func(response *tapsdk.CommitVirtualPsbtsResponse) {
				customAnchorMutateResponsePSBT(
					t, response,
					func(packet *psbt.Packet) {
						packet.UnsignedTx.TxIn[0].Sequence++
					},
				)
			},
			wantErr: "changed anchor input 0 outpoint or sequence",
		},
		{
			name: "BTC-only output script",
			mutateResponse: func(response *tapsdk.CommitVirtualPsbtsResponse) {
				customAnchorMutateResponsePSBT(
					t, response,
					func(packet *psbt.Packet) {
						packet.UnsignedTx.TxOut[0].PkScript =
							[]byte{txscript.OP_FALSE}
					},
				)
			},
			wantErr: "changed BTC-only output 0 script",
		},
		{
			name: "BTC-only output value",
			mutateResponse: func(response *tapsdk.CommitVirtualPsbtsResponse) {
				customAnchorMutateResponsePSBT(
					t, response,
					func(packet *psbt.Packet) {
						packet.UnsignedTx.TxOut[0].Value++
					},
				)
			},
			wantErr: "changed BTC-only output 0 value",
		},
		{
			name: "P2A output",
			mutateResponse: func(response *tapsdk.CommitVirtualPsbtsResponse) {
				customAnchorMutateResponsePSBT(
					t, response,
					func(packet *psbt.Packet) {
						packet.UnsignedTx.TxOut[2].PkScript =
							[]byte{txscript.OP_TRUE}
					},
				)
			},
			wantErr: "changed BTC-only output 2 script",
		},
		{
			name: "skip-funding input addition",
			mutateResponse: func(response *tapsdk.CommitVirtualPsbtsResponse) {
				customAnchorMutateResponsePSBT(
					t, response,
					func(packet *psbt.Packet) {
						packet.UnsignedTx.AddTxIn(&wire.TxIn{})
						packet.Inputs = append(
							packet.Inputs, psbt.PInput{},
						)
					},
				)
			},
			wantErr: "skip-funding commit changed anchor inputs",
		},
		{
			name: "virtual packet count",
			mutateResponse: func(response *tapsdk.CommitVirtualPsbtsResponse) {
				response.VirtualPsbts = nil
			},
			wantErr: "changed virtual packet counts",
		},
		{
			name: "virtual output script key",
			mutatePackets: func(_ *psbt.Packet,
				active []*tappsbt.VPacket) {

				output := active[0].Outputs[0]
				rawKey := testPrivateKey(t, 101).PubKey()
				outputKey := txscript.ComputeTaprootKeyNoScript(
					rawKey,
				)
				scriptKey := asset.NewScriptKey(outputKey)
				output.ScriptKey = scriptKey
				output.Asset.ScriptKey = scriptKey
				if output.SplitAsset != nil {
					output.SplitAsset.ScriptKey = scriptKey
				}
			},
			wantErr: "committed active virtual packet 0 output 0 " +
				"alternate leaves changed",
		},
		{
			name: "virtual anchor internal key",
			mutatePackets: func(anchor *psbt.Packet,
				active []*tappsbt.VPacket) {

				output := active[0].Outputs[0]
				newKey := testPrivateKey(t, 102).PubKey()
				output.AnchorOutputInternalKey = newKey
				anchor.Outputs[output.AnchorOutputIndex].
					TaprootInternalKey =
					schnorr.SerializePubKey(newKey)
			},
			wantErr: "backend changed asset output 1 PSBT metadata",
		},
		{
			name: "transition proof version downgrade",
			mutateResponse: func(response *tapsdk.CommitVirtualPsbtsResponse) {
				packet, err := tappsbt.Decode(response.VirtualPsbts[0])
				require.NoError(t, err)
				require.NotNil(t, packet.Outputs[0].ProofSuffix)
				packet.Outputs[0].ProofSuffix.Version =
					proof.TransitionV0
				response.VirtualPsbts[0], err = tappsbt.Encode(packet)
				require.NoError(t, err)
			},
			wantErr: "proof suffix mismatch",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newCustomAnchorBuilderFixture(t)
			fixture.client.signVirtual = func(_ context.Context,
				packet []byte) ([]byte, error) {

				return customAnchorSignVirtualPacket(
					t, fixture.assetSpendKey, packet,
				), nil
			}
			fixture.client.commit = func(_ context.Context,
				req *tapsdk.CommitVirtualPsbtsRequest) (
				*tapsdk.CommitVirtualPsbtsResponse, error) {

				response := customAnchorTestCommitResponseWithMutation(
					t, req, test.mutatePackets,
				)
				if test.mutateResponse != nil {
					test.mutateResponse(response)
				}
				return response, nil
			}
			capabilities := DefaultTapdCapabilities()
			client := &capableCustomAnchorBuilderTestClient{
				customAnchorBuilderTestClient: fixture.client,
				capabilities:                  &capabilities,
			}
			plan, err := newTestTxBuilder(client).Build(
				context.Background(), fixture.request,
			)
			require.NoError(t, err)

			_, err = plan.Commit(context.Background())
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestCustomAnchorInputMetadataCompatible(t *testing.T) {
	t.Parallel()

	keys := make([]*btcec.PublicKey, 3)
	for idx := range keys {
		keys[idx] = testPrivateKey(t, byte(110+idx)).PubKey()
	}
	newInput := func() psbt.PInput {
		return psbt.PInput{
			WitnessUtxo: &wire.TxOut{
				Value:    1_000,
				PkScript: []byte{txscript.OP_TRUE},
			},
			SighashType: txscript.SigHashDefault,
			Bip32Derivation: []*psbt.Bip32Derivation{
				{
					PubKey:    keys[0].SerializeCompressed(),
					Bip32Path: []uint32{1, 2},
				},
				{
					PubKey:    keys[1].SerializeCompressed(),
					Bip32Path: []uint32{3, 4},
				},
			},
			TaprootBip32Derivation: []*psbt.TaprootBip32Derivation{
				{
					XOnlyPubKey: schnorr.SerializePubKey(keys[0]),
					Bip32Path:   []uint32{5, 6},
				},
			},
			TaprootInternalKey: schnorr.SerializePubKey(keys[0]),
		}
	}

	tests := []struct {
		name   string
		mutate func(*psbt.PInput)
		want   bool
	}{
		{
			name: "unchanged",
			want: true,
		},
		{
			name: "backend derivation enrichment",
			mutate: func(input *psbt.PInput) {
				input.Bip32Derivation = append(
					input.Bip32Derivation,
					&psbt.Bip32Derivation{
						PubKey: keys[2].SerializeCompressed(),
						Bip32Path: []uint32{
							7, 8,
						},
					},
				)
				input.TaprootBip32Derivation = append(
					input.TaprootBip32Derivation,
					&psbt.TaprootBip32Derivation{
						XOnlyPubKey: schnorr.SerializePubKey(
							keys[2],
						),
						Bip32Path: []uint32{9, 10},
					},
				)
			},
			want: true,
		},
		{
			name: "derivation reorder",
			mutate: func(input *psbt.PInput) {
				input.Bip32Derivation[0],
					input.Bip32Derivation[1] =
					input.Bip32Derivation[1],
					input.Bip32Derivation[0]
			},
			want: true,
		},
		{
			name: "caller derivation removed",
			mutate: func(input *psbt.PInput) {
				input.Bip32Derivation = input.Bip32Derivation[1:]
			},
		},
		{
			name: "caller derivation changed",
			mutate: func(input *psbt.PInput) {
				input.Bip32Derivation[0].Bip32Path[0]++
			},
		},
		{
			name: "taproot derivation removed",
			mutate: func(input *psbt.PInput) {
				input.TaprootBip32Derivation = nil
			},
		},
		{
			name: "non-derivation metadata changed",
			mutate: func(input *psbt.PInput) {
				input.SighashType = txscript.SigHashSingle
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := newInput()
			after := newInput()
			if test.mutate != nil {
				test.mutate(&after)
			}

			require.Equal(t, test.want,
				customAnchorInputMetadataCompatible(before, after))
		})
	}
}

func TestPublishCustomAnchorTransferRejectsUnsignedDrift(t *testing.T) {
	t.Parallel()

	fixture := newCustomAnchorBuilderFixture(t)
	fixture.client.signVirtual = func(_ context.Context, packet []byte) (
		[]byte, error) {

		return customAnchorSignVirtualPacket(
			t, fixture.assetSpendKey, packet,
		), nil
	}
	fixture.client.commit = func(_ context.Context,
		req *tapsdk.CommitVirtualPsbtsRequest) (
		*tapsdk.CommitVirtualPsbtsResponse, error) {

		return customAnchorTestCommitResponse(t, req), nil
	}
	publishCalls := 0
	fixture.client.publish = func(context.Context,
		*tapsdk.PublishAndLogTransferRequest) (*tapsdk.AssetPacket, error) {

		publishCalls++
		return &tapsdk.AssetPacket{}, nil
	}
	capabilities := DefaultTapdCapabilities()
	client := &capableCustomAnchorBuilderTestClient{
		customAnchorBuilderTestClient: fixture.client,
		capabilities:                  &capabilities,
	}
	wallet := tapsdk.NewWallet(client, tapsdk.NetworkRegtest)
	plan, err := NewTxBuilder(wallet).Build(
		context.Background(), fixture.request,
	)
	require.NoError(t, err)
	packageSnapshot, err := plan.Commit(context.Background())
	require.NoError(t, err)

	tests := []struct {
		name   string
		mutate func(*psbt.Packet)
	}{
		{
			name: "version",
			mutate: func(packet *psbt.Packet) {
				packet.UnsignedTx.Version++
			},
		},
		{
			name: "locktime",
			mutate: func(packet *psbt.Packet) {
				packet.UnsignedTx.LockTime++
			},
		},
		{
			name: "sequence",
			mutate: func(packet *psbt.Packet) {
				packet.UnsignedTx.TxIn[0].Sequence++
			},
		},
		{
			name: "outpoint",
			mutate: func(packet *psbt.Packet) {
				packet.UnsignedTx.TxIn[0].PreviousOutPoint.Hash =
					chainhash.Hash{1}
			},
		},
		{
			name: "output value",
			mutate: func(packet *psbt.Packet) {
				packet.UnsignedTx.TxOut[0].Value++
			},
		},
		{
			name: "output script",
			mutate: func(packet *psbt.Packet) {
				packet.UnsignedTx.TxOut[0].PkScript =
					[]byte{txscript.OP_FALSE}
			},
		},
		{
			name: "output order",
			mutate: func(packet *psbt.Packet) {
				packet.UnsignedTx.TxOut[0],
					packet.UnsignedTx.TxOut[1] =
					packet.UnsignedTx.TxOut[1],
					packet.UnsignedTx.TxOut[0]
				packet.Outputs[0], packet.Outputs[1] =
					packet.Outputs[1], packet.Outputs[0]
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			finalPacket, err := decodeAnchorPSBT(
				packageSnapshot.AnchorPsbt,
			)
			require.NoError(t, err)
			test.mutate(finalPacket)
			finalBytes, err := serializePSBT(finalPacket)
			require.NoError(t, err)

			_, err = PublishTransfer(
				context.Background(), wallet, packageSnapshot, finalBytes,
			)
			require.ErrorContains(t, err, "verify final anchor PSBT")
		})
	}
	require.Zero(t, publishCalls)
}

// customAnchorBackendSignedFixture builds a caller-funded exact transfer
// whose caller input 0 is signed out of band and whose asset anchor input 1
// is left to the backend.
func customAnchorBackendSignedFixture(
	t *testing.T) *customAnchorBuilderFixture {

	t.Helper()

	fixture := newCustomAnchorBuilderFixture(t)
	fixture.request.Funding = FundingPlan{
		Mode:              FundingCallerFundedExact,
		CallerFundedExact: &CallerFundedExact{},
	}
	fixture.request.SigningPlans = []InputSigningPlan{
		{
			InputIndex:   0,
			CallerSigned: &CallerSignedPlan{},
		},
		{
			InputIndex:    1,
			BackendSigned: &BackendSignedPlan{},
		},
	}
	fixture.client.signVirtual = func(_ context.Context, packet []byte) (
		[]byte, error) {

		return customAnchorSignVirtualPacket(
			t, fixture.assetSpendKey, packet,
		), nil
	}
	fixture.client.commit = func(_ context.Context,
		req *tapsdk.CommitVirtualPsbtsRequest) (
		*tapsdk.CommitVirtualPsbtsResponse, error) {

		return customAnchorTestCommitResponse(t, req), nil
	}

	return fixture
}

// TestCustomAnchorBackendSigningRequiresCapability proves both commit and
// publish fail closed before any backend call when the backend does not
// report anchor signing.
func TestCustomAnchorBackendSigningRequiresCapability(t *testing.T) {
	t.Parallel()

	fixture := customAnchorBackendSignedFixture(t)
	commitCalls, publishCalls := 0, 0
	commit := fixture.client.commit
	fixture.client.commit = func(ctx context.Context,
		req *tapsdk.CommitVirtualPsbtsRequest) (
		*tapsdk.CommitVirtualPsbtsResponse, error) {

		commitCalls++
		return commit(ctx, req)
	}
	fixture.client.publish = func(context.Context,
		*tapsdk.PublishAndLogTransferRequest) (*tapsdk.AssetPacket, error) {

		publishCalls++
		return nil, errors.New("unexpected publish")
	}
	capabilities := DefaultTapdCapabilities()
	client := &capableCustomAnchorBuilderTestClient{
		customAnchorBuilderTestClient: fixture.client,
		capabilities:                  &capabilities,
	}
	wallet := tapsdk.NewWallet(client, tapsdk.NetworkRegtest)
	plan, err := NewTxBuilder(wallet).Build(
		context.Background(), fixture.request,
	)
	require.NoError(t, err)

	_, err = plan.Commit(context.Background())
	var capabilityErr *UnsupportedCapabilityError
	require.ErrorAs(t, err, &capabilityErr)
	require.Equal(t, CapabilityBackendAnchorSigning,
		capabilityErr.Capability)
	require.Zero(t, commitCalls)

	capabilities.BackendAnchorSigning = CapabilitySupported
	packageSnapshot, err := plan.Commit(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, commitCalls)

	capabilities.BackendAnchorSigning = CapabilityUnknown
	finalPacket := mustDecodeAnchorPSBT(t, packageSnapshot.AnchorPsbt)
	setCustomAnchorTestKeyPathWitness(
		t, finalPacket, 0, fixture.btcInputKey,
		txscript.SigHashDefault,
	)
	_, err = PublishTransfer(
		context.Background(), wallet, packageSnapshot,
		mustSerializeAnchorPSBT(t, finalPacket),
	)
	require.ErrorAs(t, err, &capabilityErr)
	require.Equal(t, CapabilityBackendAnchorSigning,
		capabilityErr.Capability)
	require.Zero(t, publishCalls)
}

// TestCustomAnchorBackendSignedPublish proves publish asks the backend to
// sign the anchor and accepts the returned transaction only if it keeps the
// committed txid and caller witnesses and every witness verifies.
func TestCustomAnchorBackendSignedPublish(t *testing.T) {
	t.Parallel()

	signAnchor := func(t *testing.T, packet *psbt.Packet,
		key *btcec.PrivateKey) {

		setCustomAnchorTestKeyPathWitness(
			t, packet, 1, key, txscript.SigHashDefault,
		)
	}
	tests := []struct {
		name string

		// respond turns the published PSBT into the backend's anchor
		// transaction bytes.
		respond func(*testing.T, *customAnchorBuilderFixture,
			*psbt.Packet) []byte

		wantErr string
	}{
		{
			name: "backend signs the anchor",
			respond: func(t *testing.T, f *customAnchorBuilderFixture,
				packet *psbt.Packet) []byte {

				signAnchor(t, packet, f.anchorInputKey)
				return customAnchorTestTxBytes(t, packet)
			},
		},
		{
			name: "anchor left unsigned",
			respond: func(t *testing.T, _ *customAnchorBuilderFixture,
				packet *psbt.Packet) []byte {

				setCustomAnchorTestWitness(
					t, &packet.Inputs[1], wire.TxWitness{},
				)
				return customAnchorTestTxBytes(t, packet)
			},
			wantErr: "verify final anchor input 1 witness",
		},
		{
			name: "anchor signed by another key",
			respond: func(t *testing.T, f *customAnchorBuilderFixture,
				packet *psbt.Packet) []byte {

				signAnchor(t, packet, f.btcInputKey)
				return customAnchorTestTxBytes(t, packet)
			},
			wantErr: "verify final anchor input 1 witness",
		},
		{
			name: "caller witness replaced",
			respond: func(t *testing.T, f *customAnchorBuilderFixture,
				packet *psbt.Packet) []byte {

				signAnchor(t, packet, f.anchorInputKey)
				setCustomAnchorTestKeyPathWitness(
					t, packet, 0, f.btcInputKey,
					txscript.SigHashAll,
				)
				return customAnchorTestTxBytes(t, packet)
			},
			wantErr: "anchor input 0 witness changed",
		},
		{
			name: "txid changed",
			respond: func(t *testing.T, f *customAnchorBuilderFixture,
				packet *psbt.Packet) []byte {

				signAnchor(t, packet, f.anchorInputKey)
				packet.UnsignedTx.LockTime++
				return customAnchorTestTxBytes(t, packet)
			},
			wantErr: "anchor transaction txid changed",
		},
		{
			name: "trailing bytes",
			respond: func(t *testing.T, f *customAnchorBuilderFixture,
				packet *psbt.Packet) []byte {

				signAnchor(t, packet, f.anchorInputKey)
				return append(customAnchorTestTxBytes(t, packet), 0)
			},
			wantErr: "trailing bytes",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := customAnchorBackendSignedFixture(t)
			var publishReq *tapsdk.PublishAndLogTransferRequest
			fixture.client.publish = func(_ context.Context,
				req *tapsdk.PublishAndLogTransferRequest) (*tapsdk.AssetPacket,
				error) {

				publishReq = cloneCustomAnchorPublishRequest(req)
				packet := mustDecodeAnchorPSBT(t, req.AnchorPsbt)

				return &tapsdk.AssetPacket{
					AnchorTransaction: test.respond(
						t, fixture, packet,
					),
					VirtualTransactions: cloneByteSlices(
						req.VirtualPsbts,
					),
					PassiveAssetTransactions: cloneByteSlices(
						req.PassiveAssetPsbts,
					),
				}, nil
			}
			capabilities := DefaultTapdCapabilities()
			capabilities.BackendAnchorSigning =
				CapabilitySupported
			client := &capableCustomAnchorBuilderTestClient{
				customAnchorBuilderTestClient: fixture.client,
				capabilities:                  &capabilities,
			}
			wallet := tapsdk.NewWallet(client, tapsdk.NetworkRegtest)
			plan, err := NewTxBuilder(wallet).Build(
				context.Background(), fixture.request,
			)
			require.NoError(t, err)
			packageSnapshot, err := plan.Commit(context.Background())
			require.NoError(t, err)

			finalPacket := mustDecodeAnchorPSBT(
				t, packageSnapshot.AnchorPsbt,
			)
			setCustomAnchorTestKeyPathWitness(
				t, finalPacket, 0, fixture.btcInputKey,
				txscript.SigHashDefault,
			)
			result, err := PublishTransfer(
				context.Background(), wallet, packageSnapshot,
				mustSerializeAnchorPSBT(t, finalPacket),
			)

			require.NotNil(t, publishReq)
			require.True(t, publishReq.SignAnchorInputs)
			sent := mustDecodeAnchorPSBT(t, publishReq.AnchorPsbt)
			require.NotEmpty(t, sent.Inputs[0].FinalScriptWitness)
			require.Empty(t, sent.Inputs[1].FinalScriptWitness)

			if test.wantErr != "" {
				var attemptErr *PublishAttemptError
				require.ErrorAs(t, err, &attemptErr)
				require.True(t, attemptErr.OutcomeUnknown)
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)

			var anchorTx wire.MsgTx
			require.NoError(t, anchorTx.Deserialize(
				bytes.NewReader(result.AnchorTransaction),
			))
			require.Equal(t, sent.UnsignedTx.TxHash(),
				anchorTx.TxHash())
			require.Len(t, anchorTx.TxIn[1].Witness, 1)
		})
	}
}

func customAnchorTestTxBytes(t *testing.T, packet *psbt.Packet) []byte {
	t.Helper()

	tx, err := psbt.Extract(packet)
	require.NoError(t, err)
	var buffer bytes.Buffer
	require.NoError(t, tx.Serialize(&buffer))

	return buffer.Bytes()
}

func customAnchorSignVirtualPacket(t *testing.T,
	privateKey *btcec.PrivateKey, packetBytes []byte) []byte {

	t.Helper()
	packet, err := tappsbt.Decode(packetBytes)
	require.NoError(t, err)
	for _, input := range packet.Inputs {
		derivation, taprootDerivation :=
			tappsbt.Bip32DerivationFromKeyDesc(
				keychain.KeyDescriptor{PubKey: privateKey.PubKey()}, 1,
			)
		input.Bip32Derivation = []*psbt.Bip32Derivation{derivation}
		input.TaprootBip32Derivation = []*psbt.TaprootBip32Derivation{
			taprootDerivation,
		}
	}
	err = tapsend.SignVirtualTransaction(
		packet, tapscript.NewMockSigner(privateKey),
		customAnchorTestWitnessValidator{},
	)
	require.NoError(t, err)
	encoded, err := tappsbt.Encode(packet)
	require.NoError(t, err)
	return encoded
}

type customAnchorTestWitnessValidator struct{}

func (customAnchorTestWitnessValidator) ValidateWitnesses(
	newAsset *asset.Asset, splitAssets []*commitment.SplitAsset,
	prevAssets commitment.InputSet) error {

	return vm.ValidateWitnesses(newAsset, splitAssets, prevAssets)
}

func customAnchorTestCommitResponse(t *testing.T,
	req *tapsdk.CommitVirtualPsbtsRequest) *tapsdk.CommitVirtualPsbtsResponse {

	return customAnchorTestCommitResponseWithMutation(
		t, req, nil,
	)
}

func customAnchorTestCommitResponseWithMutation(t *testing.T,
	req *tapsdk.CommitVirtualPsbtsRequest,
	mutate func(*psbt.Packet,
		[]*tappsbt.VPacket)) *tapsdk.CommitVirtualPsbtsResponse {

	t.Helper()
	anchor, err := decodeAnchorPSBT(req.AnchorPsbt)
	require.NoError(t, err)
	active, err := decodeVirtualPackets("active", req.VirtualPsbts)
	require.NoError(t, err)
	passive, err := decodeVirtualPackets(
		"passive", req.PassiveAssetPsbts,
	)
	require.NoError(t, err)
	allPackets := append(
		append([]*tappsbt.VPacket(nil), active...), passive...,
	)
	if mutate != nil {
		mutate(anchor, active)
	}
	outputCommitments, err := tapsend.CreateOutputCommitments(
		allPackets, tapsend.WithSpenderLeaves(),
	)
	require.NoError(t, err)
	for _, packet := range allPackets {
		require.NoError(t, tapsend.UpdateTaprootOutputKeys(
			anchor, packet, outputCommitments,
		))
	}
	for _, packet := range allPackets {
		for outputIndex := range packet.Outputs {
			suffix, err := tapsend.CreateProofSuffix(
				anchor.UnsignedTx, anchor.Outputs, packet,
				outputCommitments, outputIndex, allPackets,
				proof.WithVersion(proof.TransitionV1),
			)
			require.NoError(t, err)
			packet.Outputs[outputIndex].ProofSuffix = suffix
		}
	}

	anchorBytes, err := serializePSBT(anchor)
	require.NoError(t, err)
	activeBytes, err := encodeVirtualPackets(active)
	require.NoError(t, err)
	passiveBytes, err := encodeVirtualPackets(passive)
	require.NoError(t, err)

	return &tapsdk.CommitVirtualPsbtsResponse{
		AnchorPsbt:        anchorBytes,
		VirtualPsbts:      activeBytes,
		PassiveAssetPsbts: passiveBytes,
		ChangeOutputIndex: -1,
	}
}

func customAnchorMutateResponsePSBT(t *testing.T,
	response *tapsdk.CommitVirtualPsbtsResponse, mutate func(*psbt.Packet)) {

	t.Helper()
	packet, err := decodeAnchorPSBT(response.AnchorPsbt)
	require.NoError(t, err)
	mutate(packet)
	response.AnchorPsbt, err = serializePSBT(packet)
	require.NoError(t, err)
}

func customAnchorFinalizePSBT(t *testing.T, encoded []byte,
	keys ...*btcec.PrivateKey) []byte {

	t.Helper()

	packet, err := decodeAnchorPSBT(encoded)
	require.NoError(t, err)
	require.Len(t, keys, len(packet.Inputs))
	prevOutFetcher, prevOuts, err := anchorPrevOuts(packet)
	require.NoError(t, err)
	sigHashes := txscript.NewTxSigHashes(
		packet.UnsignedTx, prevOutFetcher,
	)
	for idx := range packet.Inputs {
		signature, err := txscript.RawTxInTaprootSignature(
			packet.UnsignedTx, sigHashes, idx, prevOuts[idx].Value,
			prevOuts[idx].PkScript, packet.Inputs[idx].TaprootMerkleRoot,
			packet.Inputs[idx].SighashType, keys[idx],
		)
		require.NoError(t, err)

		var witness bytes.Buffer
		err = psbt.WriteTxWitness(
			&witness, wire.TxWitness{signature},
		)
		require.NoError(t, err)
		packet.Inputs[idx].FinalScriptWitness = witness.Bytes()
	}
	finalized, err := serializePSBT(packet)
	require.NoError(t, err)
	extracted, err := psbt.Extract(packet)
	require.NoError(t, err)
	require.Len(t, extracted.TxIn, len(packet.Inputs))

	return finalized
}

func cloneCustomAnchorCommitRequest(
	req *tapsdk.CommitVirtualPsbtsRequest) *tapsdk.CommitVirtualPsbtsRequest {

	if req == nil {
		return nil
	}
	clone := *req
	clone.AnchorPsbt = cloneBytes(req.AnchorPsbt)
	clone.VirtualPsbts = cloneByteSlices(req.VirtualPsbts)
	clone.PassiveAssetPsbts = cloneByteSlices(req.PassiveAssetPsbts)
	clone.Funding.CustomLockID = cloneBytes(req.Funding.CustomLockID)
	return &clone
}

func cloneCustomAnchorPublishRequest(
	r *tapsdk.PublishAndLogTransferRequest) *tapsdk.PublishAndLogTransferRequest {

	if r == nil {
		return nil
	}
	clone := *r
	clone.AnchorPsbt = cloneBytes(r.AnchorPsbt)
	clone.VirtualPsbts = cloneByteSlices(r.VirtualPsbts)
	clone.PassiveAssetPsbts = cloneByteSlices(r.PassiveAssetPsbts)
	clone.LockedUTXOs = append([]tapsdk.Outpoint(nil), r.LockedUTXOs...)
	return &clone
}
