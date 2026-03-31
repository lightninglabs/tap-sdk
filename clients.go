package tapsdk

import (
	"context"

	"github.com/lightninglabs/tap-sdk/entities"
)

// Client combines all sub-clients into a single interface.
type Client interface {
	WalletClient
	ProofClient
	WalletKitClient
	UniverseClient
	MintClient
	EventClient

	Close() error
}

// WalletClient exposes the TaprootAssets service gRPC client.
type WalletClient interface {
	GetInfo(ctx context.Context) (*entities.Info, error)

	// ListAssets lists wallet assets with optional filtering.
	ListAssets(ctx context.Context,
		req *entities.ListAssetsRequest) ([]*entities.Asset, error)

	// ListBalances is a low-level wrapper around the TaprootAssets balance RPC.
	// It exposes the daemon's raw grouping modes for advanced use cases.
	// High-level SDK surfaces should still prefer the semantic asset model:
	// fungible assets by group key, collectibles by asset ID.
	ListBalances(ctx context.Context,
		req *entities.ListBalancesRequest) (*entities.ListBalancesResponse,
		error)

	// ListTransfers lists outgoing transfers with optional filtering.
	ListTransfers(ctx context.Context,
		req *entities.ListTransfersRequest) ([]*entities.AssetTransfer, error)

	// SendAsset performs a low-level one-shot address-based send using the
	// TaprootAssets service. For reusable V2 addresses, specify amounts
	// through SendAssetRequest.Recipients.
	//
	// This method intentionally exposes the raw daemon capability for advanced
	// callers. Higher-level send APIs should remain opinionated about semantic
	// asset identity and default address handling.
	SendAsset(ctx context.Context,
		req *entities.SendAssetRequest) (*entities.AssetTransfer, error)

	// NewAddr creates a new Taproot Asset address for receiving assets.
	// The address is stored in tapd and can be queried later.
	//
	// For V0/V1 addresses, AssetID and Amount are required.
	// For V2 addresses, either AssetID or GroupKey must be set.
	//
	// If ScriptKey and InternalKey are not provided, tapd derives them
	// from its internal wallet.
	NewAddr(ctx context.Context,
		req *entities.NewAddressRequest) (*entities.Address, error)

	// DecodeAddr decodes a bech32m Taproot Asset address string into its
	// components. This does not store the address or require it to be
	// previously known.
	DecodeAddr(ctx context.Context, addr string) (*entities.Address, error)

	// QueryAddrs returns addresses that were previously created by this
	// tapd instance, filtered by the query parameters.
	QueryAddrs(ctx context.Context,
		query *entities.AddressQuery) ([]*entities.Address, error)

	// AddrReceives returns incoming transfer events for addresses created
	// by this tapd instance. Use this to track the status of expected
	// incoming transfers.
	AddrReceives(ctx context.Context,
		query *entities.AddressReceivesQuery) ([]*entities.AddressEvent,
		error)

	// ListUtxos lists managed UTXOs with optional filtering.
	ListUtxos(ctx context.Context,
		req *entities.ListUtxosRequest) (
		map[string]*entities.ManagedUtxo, error)

	// ListGroups lists all known asset groups.
	ListGroups(ctx context.Context) (
		map[string]*entities.GroupedAssets, error)

	// BurnAsset burns asset units. The confirmation text must be set
	// to "assets will be destroyed" for the burn to succeed.
	BurnAsset(ctx context.Context,
		req *entities.BurnAssetRequest) (
		*entities.BurnAssetResponse, error)

	// ListBurns lists asset burns with optional filtering.
	ListBurns(ctx context.Context,
		req *entities.ListBurnsRequest) ([]*entities.AssetBurn, error)

	// FetchAssetMeta fetches the metadata for an asset by ID or meta
	// hash.
	FetchAssetMeta(ctx context.Context,
		req *entities.FetchAssetMetaRequest) (
		*entities.AssetMeta, error)

	// VerifyProof verifies a proof file and returns the decoded last
	// proof if valid.
	VerifyProof(ctx context.Context,
		rawProofFile []byte) (
		*entities.VerifyProofResponse, error)
}

// ProofClient exposes proof-related operations from the TaprootAssets service.
type ProofClient interface {
	// ExportProof exports a proof file for a specific asset output.
	// If outpoint is nil, then the latest proof for the given asset/script key
	// is exported.
	ExportProof(ctx context.Context, assetID entities.AssetID,
		scriptKey entities.PubKey,
		outpoint *entities.Outpoint) (*entities.ProofFile, error)

	// UnpackProofFile unpacks a proof file into individual proofs.
	UnpackProofFile(ctx context.Context, rawProofFile []byte) ([][]byte, error)

	// DecodeProof decodes a raw proof and returns details about it.
	DecodeProof(ctx context.Context,
		rawProof []byte) (*entities.DecodedProof, error)

	// RegisterTransfer registers an inbound transfer for an interactive send.
	// The proof must already be in the local universe before calling this.
	RegisterTransfer(ctx context.Context, assetID entities.AssetID,
		groupKey *entities.PubKey, scriptKey entities.PubKey,
		outpoint entities.Outpoint) (*entities.RegisteredAsset, error)
}

// WalletKitClient exposes the AssetWalletClient service gRPC client.
type WalletKitClient interface {
	// DeriveScriptKey derives a new script key for receiving assets.
	// The script key includes both the internal key and the tweaked
	// Taproot output key.
	DeriveScriptKey(ctx context.Context) (*entities.ScriptKey, error)

	// DeriveInternalKey derives a new internal key for anchor outputs.
	DeriveInternalKey(ctx context.Context) (*entities.InternalKey, error)

	// FundTransfer funds a virtual transaction using addresses.
	FundTransfer(ctx context.Context, recipients []entities.Recipient,
		inputs []entities.PrevID) (*entities.FundedTransfer, error)

	// FundInteractivePsbt funds a virtual PSBT for interactive sends.
	FundInteractivePsbt(ctx context.Context, psbt []byte) (
		*entities.FundedTransfer, error)

	// SignVirtualPsbt signs a virtual transaction.
	SignVirtualPsbt(ctx context.Context, fundedPsbt []byte) ([]byte, error)

	// CommitVirtualPsbts commits virtual transactions.
	CommitVirtualPsbts(ctx context.Context, virtualPsbts [][]byte,
		passivePsbts [][]byte, feeRate uint64) (*entities.CommittedTransfer,
		error)

	// AnchorVirtualPsbts anchors signed virtual PSBTs in a single call.
	// This combines committing and publishing into one operation.
	AnchorVirtualPsbts(ctx context.Context, signedPsbts [][]byte) (
		*entities.AssetTransfer, error)

	// PublishAndLogTransfer publishes the anchor transaction and logs the
	// transfer.
	PublishAndLogTransfer(ctx context.Context, anchorPsbt []byte,
		virtualPsbts [][]byte, passivePsbts [][]byte,
		skipAnchorTxBroadcast bool) (*entities.AssetPacket, error)

	// QueryInternalKey looks up an internal key by its raw public key
	// bytes. The input can be 32-byte x-only or 33-byte compressed.
	QueryInternalKey(ctx context.Context,
		internalKey []byte) (*entities.KeyDescriptor, error)

	// QueryScriptKey looks up a script key by its tweaked public key
	// bytes. The input can be 32-byte x-only or 33-byte compressed.
	QueryScriptKey(ctx context.Context,
		tweakedScriptKey []byte) (*entities.ScriptKey, error)

	// ProveAssetOwnership generates a proof of ownership for an
	// asset.
	ProveAssetOwnership(ctx context.Context,
		req *entities.ProveOwnershipRequest) (
		*entities.OwnershipProof, error)

	// VerifyAssetOwnership verifies an asset ownership proof.
	VerifyAssetOwnership(ctx context.Context,
		req *entities.VerifyOwnershipRequest) (
		*entities.VerifyOwnershipResponse, error)

	// RemoveUTXOLease removes a lease on a UTXO.
	RemoveUTXOLease(ctx context.Context,
		outpoint entities.Outpoint) error

	// DeclareScriptKey informs the wallet about an externally derived
	// script key.
	DeclareScriptKey(ctx context.Context,
		req *entities.DeclareScriptKeyRequest) (
		*entities.ScriptKey, error)
}

// UniverseClient exposes the Universe service gRPC client.
type UniverseClient interface {
	// InsertProof inserts a proof into the local universe.
	InsertProof(ctx context.Context, rawProof []byte,
		decoded *entities.DecodedProof) error

	// AssetRoots returns the known universe roots for all assets.
	AssetRoots(ctx context.Context,
		req *entities.AssetRootRequest) (map[string]*entities.UniverseRoot,
		error)

	// QueryAssetRoots queries the issuance and transfer roots for a
	// specific asset identified by its UniverseID.
	QueryAssetRoots(ctx context.Context,
		id *entities.UniverseID) (*entities.QueryRootResponse, error)

	// DeleteAssetRoot deletes a universe root and all associated data
	// for a given asset.
	DeleteAssetRoot(ctx context.Context,
		id *entities.UniverseID) error

	// AssetLeafKeys returns the set of leaf keys for a universe,
	// identified by asset ID or group key.
	AssetLeafKeys(ctx context.Context,
		req *entities.AssetLeafKeysRequest) ([]entities.AssetLeafKey,
		error)

	// AssetLeaves returns the set of asset leaves for a universe.
	AssetLeaves(ctx context.Context,
		id *entities.UniverseID) ([]entities.AssetLeaf, error)

	// QueryProof queries a specific proof from the universe by its
	// full key (universe ID + leaf key).
	QueryProof(ctx context.Context,
		key *entities.UniverseKey) (*entities.AssetProofResponse,
		error)

	// UniverseStats returns aggregate statistics for the universe.
	UniverseStats(ctx context.Context) (*entities.UniverseStats, error)

	// QueryAssetStats returns per-asset statistics filtered and sorted
	// by the given query parameters.
	QueryAssetStats(ctx context.Context,
		req *entities.AssetStatsQuery) ([]entities.AssetStatsSnapshot,
		error)

	// QueryEvents returns daily sync and proof event counts within a
	// time range.
	QueryEvents(ctx context.Context,
		req *entities.QueryEventsRequest) ([]entities.GroupedUniverseEvents,
		error)

	// ListFederationServers lists the universe federation peers.
	ListFederationServers(
		ctx context.Context) ([]entities.FederationServer, error)

	// AddFederationServer adds servers to the federation.
	AddFederationServer(ctx context.Context,
		servers []entities.FederationServer) error

	// DeleteFederationServer removes servers from the federation.
	DeleteFederationServer(ctx context.Context,
		servers []entities.FederationServer) error

	// SetFederationSyncConfig sets the federation sync configuration.
	SetFederationSyncConfig(ctx context.Context,
		global []entities.GlobalFederationSyncConfig,
		asset []entities.AssetFederationSyncConfig) error

	// QueryFederationSyncConfig queries the federation sync
	// configuration.
	QueryFederationSyncConfig(ctx context.Context,
		ids []entities.UniverseID) (*entities.FederationSyncConfig,
		error)

	// Info returns basic universe server information.
	Info(ctx context.Context) (*entities.UniverseInfo, error)

	// SyncUniverse synchronizes with a remote universe server.
	SyncUniverse(ctx context.Context,
		req *entities.SyncRequest) ([]entities.SyncedUniverse, error)
}

// MintClient exposes low-level minting operations from the Mint service gRPC
// client.
//
// These methods are building blocks for advanced callers and for future,
// more opinionated mint workflows. They should not be treated as the final
// high-level mint UX of the SDK.
type MintClient interface {
	// MintAsset stages a new asset in the pending mint batch.
	MintAsset(ctx context.Context,
		req *entities.MintAssetRequest) (*entities.MintingBatch, error)

	// FundBatch funds the current pending mint batch.
	FundBatch(ctx context.Context,
		req *entities.FundBatchRequest) (*entities.VerboseMintingBatch,
		error)

	// SealBatch seals a funded batch before finalization.
	SealBatch(ctx context.Context,
		req *entities.SealBatchRequest) (*entities.MintingBatch, error)

	// FinalizeBatch finalizes the current pending mint batch.
	FinalizeBatch(ctx context.Context,
		req *entities.FinalizeBatchRequest) (*entities.MintingBatch, error)

	// CancelBatch cancels the current mint batch.
	CancelBatch(ctx context.Context) (*entities.CancelBatchResponse, error)

	// ListBatches lists mint batches known to the daemon.
	ListBatches(ctx context.Context,
		req *entities.ListBatchesRequest) ([]*entities.VerboseMintingBatch,
		error)
}

// EventClient exposes server-streaming RPCs for real-time event
// notifications. All subscribe methods return a channel that delivers
// events until the context is cancelled or the stream encounters an
// error. The error channel receives at most one value: the terminal
// stream error (nil on clean shutdown).
type EventClient interface {
	// SubscribeReceiveEvents streams incoming asset transfer events.
	// Filter by address or start time via the request. The returned
	// channels are closed when the context is cancelled.
	SubscribeReceiveEvents(ctx context.Context,
		req *entities.SubscribeReceiveEventsRequest) (
		<-chan *entities.ReceiveEvent, <-chan error, error)

	// SubscribeSendEvents streams outgoing asset transfer events.
	// Filter by script key, label, or start time via the request.
	SubscribeSendEvents(ctx context.Context,
		req *entities.SubscribeSendEventsRequest) (
		<-chan *entities.SendEvent, <-chan error, error)

	// SubscribeMintEvents streams minting batch lifecycle events.
	SubscribeMintEvents(ctx context.Context,
		req *entities.SubscribeMintEventsRequest) (
		<-chan *entities.MintEvent, <-chan error, error)
}
