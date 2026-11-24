package embedded

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/lightninglabs/taproot-assets/address"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/commitment"
	"github.com/lightninglabs/taproot-assets/proof"
	"github.com/lightninglabs/taproot-assets/rpcutils"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// addressServer adds the address calls of tapd's TaprootAssets service to
// walletServer. Only a host that set a ReceiveConfig gets it, so the calls
// stay unimplemented for every other host.
type addressServer struct {
	*walletServer

	addrs AddressStore

	// defaultCourier is nil when the host configured none.
	defaultCourier *url.URL
}

// newAddressServer returns the wallet service with the address calls.
func newAddressServer(wallet *walletServer, cfg *ReceiveConfig) *addressServer {
	server := &addressServer{
		walletServer: wallet,
		addrs:        cfg.Store,
	}
	if cfg.DefaultCourier != nil {
		courier := *cfg.DefaultCourier
		server.defaultCourier = &courier
	}

	return server
}

// NewAddr creates a version 2 address under the next two keys of the wallet
// and records it. The address requests asset version 1, whichever version
// the request names.
func (s *addressServer) NewAddr(ctx context.Context,
	req *taprpc.NewAddrRequest) (*taprpc.Addr, error) {

	switch req.AddressVersion {
	case taprpc.AddrVersion_ADDR_VERSION_UNSPECIFIED,
		taprpc.AddrVersion_ADDR_VERSION_V2:

	default:
		return nil, status.Errorf(codes.InvalidArgument, "address "+
			"version %v is not supported, the wallet creates "+
			"version 2 addresses only", req.AddressVersion)
	}

	if req.ScriptKey != nil || req.InternalKey != nil ||
		len(req.TapscriptSibling) != 0 {

		return nil, status.Error(codes.Unimplemented, "caller keys "+
			"and tapscript siblings are not supported")
	}

	assetID, groupKey, err := addrAsset(req)
	if err != nil {
		return nil, err
	}
	courier, err := s.courier(req.ProofCourierAddr)
	if err != nil {
		return nil, err
	}

	height, _, err := s.engine.chain.BestBlock(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch best block: %w", err)
	}

	scriptKey, err := s.engine.nextInternalKey(
		ctx, asset.TaprootAssetsKeyFamily,
	)
	if err != nil {
		return nil, fmt.Errorf("error deriving script key: %w", err)
	}
	internalKey, err := s.engine.nextInternalKey(
		ctx, asset.TaprootAssetsKeyFamily,
	)
	if err != nil {
		return nil, fmt.Errorf("error deriving internal key: %w", err)
	}

	// address.New needs the asset's genesis and group witness, which the
	// wallet has only for an asset it holds. Neither is encoded.
	addr := &address.Tap{
		Version:     address.V2,
		ChainParams: &s.engine.params,

		// A version 0 output cannot be the base of an unconfirmed
		// proof path or a passive asset of a custom-anchor transfer.
		// The request's version is not read: its zero value is
		// version 0.
		AssetVersion: asset.V1,
		AssetID:      assetID,
		GroupKey:     groupKey,

		// The sender derives the script key of each asset output from
		// the raw key, so the address names no BIP-86 output key.
		ScriptKey:        *scriptKey.PubKey,
		InternalKey:      *internalKey.PubKey,
		Amount:           req.Amt,
		ProofCourierAddr: *courier,
	}
	rpcAddr, err := marshalAddr(addr)
	if err != nil {
		return nil, err
	}

	record := AddressRecord{
		ScriptKey:      asset.ToSerialized(scriptKey.PubKey),
		Encoded:        rpcAddr.Encoded,
		InternalKey:    asset.ToSerialized(internalKey.PubKey),
		CreationHeight: height,
		CreatedAt:      time.Now(),
	}
	if err := s.addrs.InsertAddress(ctx, record); err != nil {
		return nil, fmt.Errorf("error storing address: %w", err)
	}

	if groupKey != nil {
		s.engine.trustAddressGroup(groupKey)
	}
	s.engine.receive.subscribe(record)

	return rpcAddr, nil
}

// addrAsset returns the asset a version 2 address is for: an asset ID, or a
// group key with a zero asset ID.
func addrAsset(req *taprpc.NewAddrRequest) (asset.ID, *btcec.PublicKey,
	error) {

	var assetID asset.ID

	switch {
	case len(req.AssetId) != 0 && len(req.GroupKey) != 0:
		return assetID, nil, status.Error(codes.InvalidArgument,
			"version 2 addresses for grouped assets must use "+
				"group key only")

	case len(req.AssetId) != 0:
		if len(req.AssetId) != len(assetID) {
			return assetID, nil, status.Error(
				codes.InvalidArgument,
				"asset ID must be 32 bytes",
			)
		}
		copy(assetID[:], req.AssetId)

		// An address encodes a zero asset ID as no asset at all.
		if assetID == asset.ZeroID {
			return assetID, nil, status.Error(
				codes.InvalidArgument,
				"asset ID must not be zero",
			)
		}

		return assetID, nil, nil

	case len(req.GroupKey) != 0:
		groupKey, err := btcec.ParsePubKey(req.GroupKey)
		if err != nil {
			return assetID, nil, status.Errorf(
				codes.InvalidArgument,
				"error parsing group key: %v", err,
			)
		}

		return assetID, groupKey, nil

	default:
		return assetID, nil, status.Error(codes.InvalidArgument,
			"asset ID or group key must be specified")
	}
}

// courier returns the proof courier of a new address: the requested one, or
// the default when the request names none.
func (s *addressServer) courier(requested string) (*url.URL, error) {
	if requested == "" {
		if s.defaultCourier == nil {
			return nil, status.Error(codes.FailedPrecondition,
				"no proof courier address provided and no "+
					"default proof courier configured")
		}

		return s.defaultCourier, nil
	}

	courier, err := url.ParseRequestURI(requested)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid "+
			"proof courier address: %v", err)
	}
	if err := validateCourier(courier); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid "+
			"proof courier address: %v", err)
	}

	return courier, nil
}

// validateCourier checks that a version 2 address can name the proof
// courier, which must run an auth mailbox.
func validateCourier(courier *url.URL) error {
	if err := proof.ValidateCourierAddress(courier); err != nil {
		return err
	}
	if courier.Scheme != proof.AuthMailboxUniRpcCourierType {
		return fmt.Errorf("%w: address version %d must use the '%s' "+
			"proof courier type",
			address.ErrInvalidProofCourierAddr, address.V2,
			proof.AuthMailboxUniRpcCourierType)
	}

	// tapd's check accepts a courier that names a port and no host,
	// which no sender can reach.
	if courier.Hostname() == "" {
		return fmt.Errorf("proof courier URI address host unspecified")
	}

	return nil
}

// DecodeAddr decodes a version 2 address of the wallet's network.
func (s *addressServer) DecodeAddr(_ context.Context,
	req *taprpc.DecodeAddrRequest) (*taprpc.Addr, error) {

	if len(req.Addr) == 0 {
		return nil, status.Error(codes.InvalidArgument,
			"must specify an addr")
	}

	addr, err := address.DecodeAddress(req.Addr, &s.engine.params)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument,
			"unable to decode addr: %v", err)
	}

	// Below version 2 the Taproot output key of the response commits to
	// the asset's genesis, which the address does not encode.
	if addr.Version != address.V2 {
		return nil, status.Errorf(codes.InvalidArgument, "address "+
			"version %d is not supported, the wallet handles "+
			"version 2 addresses only", addr.Version)
	}

	return marshalAddr(addr)
}

// QueryAddrs lists the addresses the wallet created, oldest first.
func (s *addressServer) QueryAddrs(ctx context.Context,
	req *taprpc.QueryAddrRequest) (*taprpc.QueryAddrResponse, error) {

	if req.Limit < 0 || req.Offset < 0 {
		return nil, status.Error(codes.InvalidArgument,
			"limit and offset must not be negative")
	}

	records, err := s.addrs.ListAddresses(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to query addrs: %w", err)
	}

	resp := &taprpc.QueryAddrResponse{}
	skip := req.Offset
	for idx := range records {
		record := &records[idx]

		created := record.CreatedAt
		if req.CreatedAfter > 0 &&
			created.Before(time.Unix(req.CreatedAfter, 0)) {

			continue
		}
		if req.CreatedBefore > 0 &&
			created.After(time.Unix(req.CreatedBefore, 0)) {

			continue
		}

		if skip > 0 {
			skip--
			continue
		}
		if req.Limit > 0 && len(resp.Addrs) == int(req.Limit) {
			break
		}

		addr, err := address.DecodeAddress(
			record.Encoded, &s.engine.params,
		)
		if err != nil {
			return nil, fmt.Errorf("unable to decode stored addr: "+
				"%w", err)
		}
		rpcAddr, err := marshalAddr(addr)
		if err != nil {
			return nil, err
		}
		resp.Addrs = append(resp.Addrs, rpcAddr)
	}

	return resp, nil
}

// marshalAddr converts a version 2 address to its RPC form. The asset type is
// the zero value, since the address does not encode it.
func marshalAddr(addr *address.Tap) (*taprpc.Addr, error) {
	encoded, err := addr.EncodeAddress()
	if err != nil {
		return nil, fmt.Errorf("unable to encode addr: %w", err)
	}

	outputKey, err := addr.TaprootOutputKey()
	if err != nil {
		return nil, fmt.Errorf("error deriving Taproot output key: %w",
			err)
	}

	siblingBytes, _, err := commitment.MaybeEncodeTapscriptPreimage(
		addr.TapscriptSibling,
	)
	if err != nil {
		return nil, fmt.Errorf("error encoding tapscript sibling: %w",
			err)
	}

	assetVersion, err := rpcutils.MarshalAssetVersion(addr.AssetVersion)
	if err != nil {
		return nil, err
	}
	addrVersion, err := address.MarshalVersion(addr.Version)
	if err != nil {
		return nil, err
	}

	rpcAddr := &taprpc.Addr{
		Encoded:          encoded,
		Amount:           addr.Amount,
		ScriptKey:        addr.ScriptKey.SerializeCompressed(),
		InternalKey:      addr.InternalKey.SerializeCompressed(),
		TapscriptSibling: siblingBytes,
		TaprootOutputKey: schnorr.SerializePubKey(outputKey),
		ProofCourierAddr: addr.ProofCourierAddr.String(),
		AssetVersion:     assetVersion,
		AddressVersion:   addrVersion,
	}
	if addr.AssetID != asset.ZeroID {
		rpcAddr.AssetId = addr.AssetID[:]
	}
	if addr.GroupKey != nil {
		rpcAddr.GroupKey = addr.GroupKey.SerializeCompressed()
	}

	return rpcAddr, nil
}
