package entities

// ReceiveEvent represents an incoming asset transfer event.
type ReceiveEvent struct {
	// Timestamp is the event creation time as a Unix timestamp in
	// microseconds.
	Timestamp int64

	// Address is the Taproot Asset address that received the asset.
	Address *Address

	// Outpoint is the outpoint of the on-chain transaction used to
	// receive the asset.
	Outpoint string

	// Status is the current status of the receive event.
	Status AddressEventStatus

	// ConfirmationHeight is the block height at which the receive
	// transaction was confirmed. Only set when Status is
	// AddressEventStatusTransactionConfirmed or later.
	ConfirmationHeight uint32

	// Error is an optional error string indicating that processing
	// the event at the current status failed.
	Error string
}

// ParcelType represents the type of an outbound send parcel.
type ParcelType uint8

const (
	// ParcelTypeAddress is a parcel sent to one or more addresses.
	ParcelTypeAddress ParcelType = 0

	// ParcelTypePreSigned is a parcel created from pre-signed
	// virtual packets.
	ParcelTypePreSigned ParcelType = 1

	// ParcelTypePending is a pending parcel that has not yet been
	// broadcast.
	ParcelTypePending ParcelType = 2

	// ParcelTypePreAnchored is a parcel that was pre-anchored.
	ParcelTypePreAnchored ParcelType = 3
)

// AnchorTransaction contains on-chain anchor transaction details for a
// send event.
type AnchorTransaction struct {
	// AnchorPsbt is the anchor transaction PSBT packet.
	AnchorPsbt []byte

	// ChangeOutputIndex is the index of the change output, or -1 if
	// no change was produced.
	ChangeOutputIndex int32

	// ChainFeesSats is the total on-chain fees paid in satoshis.
	ChainFeesSats int64

	// TargetFeeRateSatKw is the target fee rate in sat/kWU.
	TargetFeeRateSatKw int32

	// LndLockedUtxos lists the UTXO lock leases acquired from lnd.
	LndLockedUtxos []Outpoint

	// FinalTx is the signed anchor transaction that was broadcast.
	FinalTx []byte
}

// SendState is the current state of an outbound send event. tapd
// exposes these as stable string names over the API, so the SDK keeps a
// string-backed type here as well.
type SendState string

const (
	// SendStateStartHandleAddrParcel is the initial send state.
	SendStateStartHandleAddrParcel SendState = "SendStateStartHandleAddrParcel" //nolint:lll

	// SendStateVirtualCommitmentSelect performs input coin selection.
	SendStateVirtualCommitmentSelect SendState = "SendStateVirtualCommitmentSelect" //nolint:lll

	// SendStateVirtualSign creates the asset-level witness data.
	SendStateVirtualSign SendState = "SendStateVirtualSign"

	// SendStateAnchorSign signs and finalizes the anchor PSBT.
	SendStateAnchorSign SendState = "SendStateAnchorSign"

	// SendStateVerifyPreBroadcast runs final pre-broadcast checks.
	SendStateVerifyPreBroadcast SendState = "SendStateVerifyPreBroadcast"

	// SendStateStorePreBroadcast persists the signed transaction
	// before broadcast.
	SendStateStorePreBroadcast SendState = "SendStateStorePreBroadcast"

	// SendStateBroadcast broadcasts the anchor transaction.
	SendStateBroadcast SendState = "SendStateBroadcast"

	// SendStateWaitTxConf waits for the anchor transaction to confirm.
	SendStateWaitTxConf SendState = "SendStateWaitTxConf"

	// SendStateStorePostAnchorTxConf stores post-confirmation state.
	SendStateStorePostAnchorTxConf SendState = "SendStateStorePostAnchorTxConf" //nolint:lll

	// SendStateTransferProofs transfers proofs to the receivers.
	SendStateTransferProofs SendState = "SendStateTransferProofs"

	// SendStateComplete means the transfer has completed fully.
	SendStateComplete SendState = "SendStateComplete"
)

// SendEvent represents an outgoing asset transfer event.
type SendEvent struct {
	// Timestamp is the event execution time as a Unix timestamp in
	// microseconds.
	Timestamp int64

	// SendState is the last send state that was executed
	// successfully. If Error is set, this is the state that caused
	// the failure.
	SendState SendState

	// ParcelType is the type of the outbound parcel.
	ParcelType ParcelType

	// Addresses lists the recipient addresses (not including change
	// back to own wallet). Only set for ParcelTypeAddress.
	Addresses []*Address

	// VirtualPackets are the raw virtual packets in the parcel.
	VirtualPackets [][]byte

	// PassiveVirtualPackets are passive virtual packets carried
	// alongside the active ones when other assets shared the input
	// commitment.
	PassiveVirtualPackets [][]byte

	// AnchorTransaction contains on-chain anchor details. Only set
	// after the anchor signing state.
	AnchorTransaction *AnchorTransaction

	// Transfer is the final transfer record. Only set after the
	// commitment is logged.
	Transfer *AssetTransfer

	// Error is an optional error string.
	Error string

	// TransferLabel is the label assigned to this transfer.
	TransferLabel string

	// NextSendState is the next state that will be executed.
	NextSendState SendState
}

// MintEvent represents a minting batch event.
type MintEvent struct {
	// Timestamp is the event execution time as a Unix timestamp in
	// microseconds.
	Timestamp int64

	// BatchState is the last batch state that was executed. If Error
	// is set, this is the state that caused the failure.
	BatchState BatchState

	// Batch contains the minting batch details.
	Batch *MintingBatch

	// Error is an optional error string.
	Error string
}

// SubscribeReceiveEventsRequest contains parameters for subscribing to
// incoming transfer events.
type SubscribeReceiveEventsRequest struct {
	// FilterAddr restricts events to a specific Taproot Asset
	// address. Leave empty to receive all events.
	FilterAddr string

	// StartTimestamp is the start time as a Unix timestamp in
	// microseconds. If zero, the daemon streams from the current
	// time.
	StartTimestamp int64
}

// SubscribeSendEventsRequest contains parameters for subscribing to
// outgoing transfer events.
type SubscribeSendEventsRequest struct {
	// FilterScriptKey restricts events to a specific recipient
	// script key. Leave empty to receive all send events.
	FilterScriptKey []byte

	// FilterLabel restricts events to a specific transfer label.
	// Leave empty to not filter by label.
	FilterLabel string

	// StartTimestamp is the start time as a Unix timestamp in
	// microseconds. If zero, the daemon streams from the current
	// time.
	StartTimestamp int64
}

// SubscribeMintEventsRequest contains parameters for subscribing to
// mint batch events.
type SubscribeMintEventsRequest struct {
	// ShortResponse omits the full asset list from batch events
	// to reduce data volume for large batches.
	ShortResponse bool
}
