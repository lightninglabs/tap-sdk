package embedded

import (
	"net/url"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	tapsdk "github.com/lightninglabs/tap-sdk"
	"github.com/lightninglabs/taproot-assets/address"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/taprpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// testCourier is the default proof courier of an address wallet.
	testCourier = "authmailbox+universerpc://courier.test:10029"

	// testOtherCourier is a proof courier a request names.
	testOtherCourier = "authmailbox+universerpc://other.test:10029"
)

// newAddressHarness starts an embedded client that creates addresses. An
// empty default courier configures none.
func newAddressHarness(t *testing.T, defaultCourier string) *testHarness {
	t.Helper()

	h := newTestHarness(t)
	h.addresses = &ReceiveConfig{Store: h.store}
	if defaultCourier != "" {
		courier, err := url.Parse(defaultCourier)
		require.NoError(t, err)
		h.addresses.DefaultCourier = courier
	}
	h.restart()

	return h
}

// addressServer returns the address service of the harness client.
func (h *testHarness) addressServer() *addressServer {
	return newAddressServer(
		&walletServer{engine: h.client.engine}, h.addresses,
	)
}

// addressKey returns the wallet's asset key at an index.
func (h *testHarness) addressKey(index uint32) *btcec.PublicKey {
	return h.keys.privKey(asset.TaprootAssetsKeyFamily, index).PubKey()
}

// TestNewAddr creates addresses for an asset ID and for a group key, and
// reads every field back with tapd's decoder.
func TestNewAddr(t *testing.T) {
	var (
		v2       = tapsdk.AddressVersionV2
		assetID  = tapsdk.AssetID{1, 2, 3}
		groupKey = newTestKeyRing(4).privKey(1, 0).PubKey()
		groupRef = tapsdk.AssetRefFromGroupKey(sdkPubKey(t, groupKey))
	)

	tests := []struct {
		name string

		// create makes the address under test.
		create func(h *testHarness) (*tapsdk.Address, error)

		ref      tapsdk.AssetRef
		assetID  asset.ID
		groupKey *btcec.PublicKey
		amount   uint64
		courier  string
	}{{
		name: "asset ID",
		create: func(h *testHarness) (*tapsdk.Address, error) {
			return h.client.NewAddr(
				t.Context(), &tapsdk.NewAddressRequest{
					AssetRef: tapsdk.AssetRefFromAssetID(
						assetID,
					),
					Amount:           21,
					ProofCourierAddr: testOtherCourier,
					AddressVersion:   &v2,
				},
			)
		},
		ref:     tapsdk.AssetRefFromAssetID(assetID),
		assetID: asset.ID(assetID),
		amount:  21,
		courier: testOtherCourier,
	}, {
		name: "group key",
		create: func(h *testHarness) (*tapsdk.Address, error) {
			return h.wallet.NewReceiveAddress(t.Context(), groupRef)
		},
		ref:      groupRef,
		groupKey: groupKey,
		courier:  testCourier,
	}, {
		name: "no version",
		create: func(h *testHarness) (*tapsdk.Address, error) {
			return h.client.NewAddr(
				t.Context(), &tapsdk.NewAddressRequest{
					AssetRef: groupRef,
				},
			)
		},
		ref:      groupRef,
		groupKey: groupKey,
		courier:  testCourier,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newAddressHarness(t, testCourier)
			ctx := t.Context()

			addr, err := tc.create(h)
			require.NoError(t, err)

			// The address names the raw key, not its BIP-86 output
			// key, and both keys are the next ones of the wallet.
			scriptKey := h.addressKey(0)
			internalKey := h.addressKey(1)

			decoded, err := address.DecodeAddress(
				addr.Encoded, &address.RegressionNetTap,
			)
			require.NoError(t, err)
			require.Equal(t, address.V2, decoded.Version)
			require.Equal(t, asset.V1, decoded.AssetVersion)
			require.Equal(t, tc.assetID, decoded.AssetID)
			if tc.groupKey == nil {
				require.Nil(t, decoded.GroupKey)
			} else {
				require.True(t, tc.groupKey.IsEqual(
					decoded.GroupKey,
				))
			}
			require.True(t, scriptKey.IsEqual(&decoded.ScriptKey))
			require.True(
				t, internalKey.IsEqual(&decoded.InternalKey),
			)
			require.Nil(t, decoded.TapscriptSibling)
			require.Equal(t, tc.amount, decoded.Amount)
			courier := decoded.ProofCourierAddr.String()
			require.Equal(t, tc.courier, courier)
			require.Empty(t, decoded.UnknownOddTypes)

			require.Equal(t, &tapsdk.Address{
				Encoded:     addr.Encoded,
				AssetRef:    tc.ref,
				AssetType:   tapsdk.AssetTypeFungible,
				Amount:      tc.amount,
				ScriptKey:   sdkPubKey(t, scriptKey),
				InternalKey: sdkPubKey(t, internalKey),
				TaprootOutputKey: tapsdk.XOnlyPubKey(
					schnorr.SerializePubKey(scriptKey),
				),
				ProofCourierAddr: tc.courier,
				AssetVersion:     tapsdk.AssetVersionV1,
				AddressVersion:   tapsdk.AddressVersionV2,
			}, addr)

			// The wallet can derive both keys again.
			for index, key := range []*btcec.PublicKey{
				scriptKey, internalKey,
			} {
				record, err := h.store.FetchInternalKey(
					ctx, asset.ToSerialized(key),
				)
				require.NoError(t, err)
				require.EqualValues(
					t, asset.TaprootAssetsKeyFamily,
					record.Family,
				)
				require.EqualValues(t, index, record.Index)
			}

			height, _, err := h.chain.BestBlock(ctx)
			require.NoError(t, err)
			records, err := h.store.ListAddresses(ctx)
			require.NoError(t, err)
			require.Len(t, records, 1)
			created := records[0].CreatedAt
			require.WithinDuration(
				t, time.Now(), created, time.Minute,
			)
			records[0].CreatedAt = time.Time{}
			require.Equal(t, AddressRecord{
				ScriptKey:      asset.ToSerialized(scriptKey),
				Encoded:        addr.Encoded,
				InternalKey:    asset.ToSerialized(internalKey),
				CreationHeight: height,
			}, records[0])

			fromDecode, err := h.client.DecodeAddr(
				ctx, addr.Encoded,
			)
			require.NoError(t, err)
			require.Equal(t, addr, fromDecode)

			fromQuery, err := h.client.QueryAddrs(ctx, nil)
			require.NoError(t, err)
			require.Equal(t, []*tapsdk.Address{addr}, fromQuery)
		})
	}
}

// TestNewAddrRejects refuses the requests the engine cannot serve before it
// derives a key.
func TestNewAddrRejects(t *testing.T) {
	var (
		assetID  = make([]byte, 32)
		groupKey = newTestKeyRing(4).privKey(1, 0).PubKey().
				SerializeCompressed()
	)
	assetID[0] = 1

	tests := []struct {
		name string

		// noDefaultCourier leaves the wallet without a default courier.
		noDefaultCourier bool

		req  *taprpc.NewAddrRequest
		code codes.Code
	}{{
		name: "version 0",
		req: &taprpc.NewAddrRequest{
			AssetId:        assetID,
			Amt:            1,
			AddressVersion: taprpc.AddrVersion_ADDR_VERSION_V0,
		},
		code: codes.InvalidArgument,
	}, {
		name: "version 1",
		req: &taprpc.NewAddrRequest{
			AssetId:        assetID,
			Amt:            1,
			AddressVersion: taprpc.AddrVersion_ADDR_VERSION_V1,
		},
		code: codes.InvalidArgument,
	}, {
		name: "wrong courier scheme",
		req: &taprpc.NewAddrRequest{
			AssetId:          assetID,
			ProofCourierAddr: "universerpc://courier.test:10029",
		},
		code: codes.InvalidArgument,
	}, {
		name: "courier without port",
		req: &taprpc.NewAddrRequest{
			AssetId: assetID,
			ProofCourierAddr: "authmailbox+universerpc://" +
				"courier.test",
		},
		code: codes.InvalidArgument,
	}, {
		name: "courier without host",
		req: &taprpc.NewAddrRequest{
			AssetId:          assetID,
			ProofCourierAddr: "authmailbox+universerpc://:10029",
		},
		code: codes.InvalidArgument,
	}, {
		name:             "no courier",
		noDefaultCourier: true,
		req:              &taprpc.NewAddrRequest{AssetId: assetID},
		code:             codes.FailedPrecondition,
	}, {
		name: "asset ID and group key",
		req: &taprpc.NewAddrRequest{
			AssetId:  assetID,
			GroupKey: groupKey,
		},
		code: codes.InvalidArgument,
	}, {
		name: "no asset",
		req:  &taprpc.NewAddrRequest{},
		code: codes.InvalidArgument,
	}, {
		name: "short asset ID",
		req:  &taprpc.NewAddrRequest{AssetId: assetID[:31]},
		code: codes.InvalidArgument,
	}, {
		name: "zero asset ID",
		req:  &taprpc.NewAddrRequest{AssetId: make([]byte, 32)},
		code: codes.InvalidArgument,
	}, {
		name: "invalid group key",
		req:  &taprpc.NewAddrRequest{GroupKey: groupKey[:32]},
		code: codes.InvalidArgument,
	}, {
		name: "caller script key",
		req: &taprpc.NewAddrRequest{
			AssetId:   assetID,
			ScriptKey: &taprpc.ScriptKey{PubKey: groupKey},
		},
		code: codes.Unimplemented,
	}, {
		name: "caller internal key",
		req: &taprpc.NewAddrRequest{
			AssetId: assetID,
			InternalKey: &taprpc.KeyDescriptor{
				RawKeyBytes: groupKey,
			},
		},
		code: codes.Unimplemented,
	}, {
		name: "tapscript sibling",
		req: &taprpc.NewAddrRequest{
			AssetId:          assetID,
			TapscriptSibling: []byte{1},
		},
		code: codes.Unimplemented,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defaultCourier := testCourier
			if tc.noDefaultCourier {
				defaultCourier = ""
			}
			h := newAddressHarness(t, defaultCourier)
			ctx := t.Context()

			_, err := h.addressServer().NewAddr(ctx, tc.req)
			require.Equal(t, tc.code, status.Code(err), "%v", err)

			require.Empty(t, h.keys.next)
			records, err := h.store.ListAddresses(ctx)
			require.NoError(t, err)
			require.Empty(t, records)
		})
	}
}

// TestDecodeAddr decodes any version 2 address of the wallet's network, and
// rejects what is not one.
func TestDecodeAddr(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	ctx := t.Context()

	// An address of another wallet decodes, since nothing is looked up.
	peer := newAddressHarness(t, testOtherCourier)
	foreign, err := peer.wallet.NewReceiveAddress(
		ctx, tapsdk.AssetRefFromAssetID(tapsdk.AssetID{7}),
	)
	require.NoError(t, err)

	decoded, err := h.client.DecodeAddr(ctx, foreign.Encoded)
	require.NoError(t, err)
	require.Equal(t, foreign, decoded)

	courier, err := url.Parse("universerpc://courier.test:10029")
	require.NoError(t, err)

	// encode returns an address that tapd's decoder reads on its network.
	encode := func(version address.Version,
		params *address.ChainParams) string {

		addr := address.Tap{
			Version:          version,
			ChainParams:      params,
			AssetVersion:     asset.V0,
			AssetID:          asset.ID{7},
			ScriptKey:        *h.addressKey(0),
			InternalKey:      *h.addressKey(1),
			Amount:           1,
			ProofCourierAddr: *courier,
		}
		encoded, err := addr.EncodeAddress()
		require.NoError(t, err)

		_, err = address.DecodeAddress(encoded, params)
		require.NoError(t, err)

		return encoded
	}

	// A tapd's version 2 address requests asset version 0 by default.
	assetV0, err := h.client.DecodeAddr(
		ctx, encode(address.V2, &address.RegressionNetTap),
	)
	require.NoError(t, err)
	require.Equal(t, tapsdk.AddressVersionV2, assetV0.AddressVersion)
	require.Equal(t, tapsdk.AssetVersionV0, assetV0.AssetVersion)

	tests := []struct {
		name string
		addr string

		// errText is part of the error.
		errText string
	}{{
		name:    "empty",
		errText: "must specify an addr",
	}, {
		name:    "not an address",
		addr:    "not an address",
		errText: "unable to decode addr",
	}, {
		name:    "other network",
		addr:    encode(address.V2, &address.MainNetTap),
		errText: "unable to decode addr",
	}, {
		name:    "version 0",
		addr:    encode(address.V0, &address.RegressionNetTap),
		errText: "address version 0 is not supported",
	}, {
		name:    "version 1",
		addr:    encode(address.V1, &address.RegressionNetTap),
		errText: "address version 1 is not supported",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.client.DecodeAddr(ctx, tc.addr)
			require.Equal(
				t, codes.InvalidArgument, status.Code(err),
				"%v", err,
			)
			require.ErrorContains(t, err, tc.errText)
		})
	}
}

// TestQueryAddrs lists the wallet's addresses oldest first, within the
// creation times and the page a query names.
func TestQueryAddrs(t *testing.T) {
	h := newAddressHarness(t, testCourier)
	ctx := t.Context()

	const first = int64(1_700_000_000)
	var created []*tapsdk.Address
	for idx := range 3 {
		addr, err := h.wallet.NewReceiveAddress(
			ctx, tapsdk.AssetRefFromAssetID(
				tapsdk.AssetID{byte(idx + 1)},
			),
		)
		require.NoError(t, err)
		created = append(created, addr)

		// The addresses are created ten seconds apart.
		h.store.addresses[idx].CreatedAt = time.Unix(
			first+int64(idx)*10, 0,
		)
	}

	tests := []struct {
		name  string
		query *tapsdk.AddressQuery
		want  []*tapsdk.Address
	}{{
		name: "all",
		want: created,
	}, {
		name:  "limit",
		query: &tapsdk.AddressQuery{Limit: 2},
		want:  created[:2],
	}, {
		name:  "offset",
		query: &tapsdk.AddressQuery{Offset: 1},
		want:  created[1:],
	}, {
		name:  "limit and offset",
		query: &tapsdk.AddressQuery{Limit: 1, Offset: 1},
		want:  created[1:2],
	}, {
		name:  "offset past the end",
		query: &tapsdk.AddressQuery{Offset: 3},
		want:  []*tapsdk.Address{},
	}, {
		name:  "created after",
		query: &tapsdk.AddressQuery{CreatedAfter: first + 10},
		want:  created[1:],
	}, {
		name:  "created before",
		query: &tapsdk.AddressQuery{CreatedBefore: first + 10},
		want:  created[:2],
	}, {
		name: "created between",
		query: &tapsdk.AddressQuery{
			CreatedAfter:  first + 1,
			CreatedBefore: first + 19,
		},
		want: created[1:2],
	}, {
		name: "page of a time range",
		query: &tapsdk.AddressQuery{
			CreatedAfter: first + 10,
			Limit:        1,
			Offset:       1,
		},
		want: created[2:],
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			addrs, err := h.client.QueryAddrs(ctx, tc.query)
			require.NoError(t, err)
			require.Equal(t, tc.want, addrs)
		})
	}

	for _, query := range []*tapsdk.AddressQuery{
		{Limit: -1}, {Offset: -1},
	} {
		_, err := h.client.QueryAddrs(ctx, query)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	}
}

// TestAddressCallsNeedReceiveConfig leaves every address call unimplemented
// for a host that set no ReceiveConfig, and the receive events for one that
// did.
func TestAddressCallsNeedReceiveConfig(t *testing.T) {
	ctx := t.Context()
	ref := tapsdk.AssetRefFromAssetID(tapsdk.AssetID{1})

	enabled := newAddressHarness(t, testCourier)
	addr, err := enabled.wallet.NewReceiveAddress(ctx, ref)
	require.NoError(t, err)

	_, err = enabled.client.AddrReceives(ctx, nil)
	require.Equal(t, codes.Unimplemented, status.Code(err))

	h := newTestHarness(t)

	_, err = h.wallet.NewReceiveAddress(ctx, ref)
	require.Equal(t, codes.Unimplemented, status.Code(err))

	v2 := tapsdk.AddressVersionV2
	_, err = h.client.NewAddr(ctx, &tapsdk.NewAddressRequest{
		AssetRef:         ref,
		ProofCourierAddr: testCourier,
		AddressVersion:   &v2,
	})
	require.Equal(t, codes.Unimplemented, status.Code(err))

	_, err = h.client.DecodeAddr(ctx, addr.Encoded)
	require.Equal(t, codes.Unimplemented, status.Code(err))

	_, err = h.client.QueryAddrs(ctx, nil)
	require.Equal(t, codes.Unimplemented, status.Code(err))

	_, err = h.client.AddrReceives(ctx, nil)
	require.Equal(t, codes.Unimplemented, status.Code(err))

	// The wallet derived no key for the refused requests.
	require.Empty(t, h.keys.next)
}

// TestReceiveConfigValidation refuses a receive configuration without a
// store or with a default courier no address can name.
func TestReceiveConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		receive *ReceiveConfig
		errText string
	}{{
		name:    "no store",
		receive: &ReceiveConfig{},
		errText: "address store is required",
	}, {
		name: "universe courier",
		receive: &ReceiveConfig{
			Store: NewMemoryStore(),
			DefaultCourier: &url.URL{
				Scheme: "universerpc",
				Host:   "courier.test:10029",
			},
		},
		errText: "invalid proof courier address",
	}, {
		name: "courier without port",
		receive: &ReceiveConfig{
			Store: NewMemoryStore(),
			DefaultCourier: &url.URL{
				Scheme: "authmailbox+universerpc",
				Host:   "courier.test",
			},
		},
		errText: "port unspecified",
	}, {
		name: "courier without host",
		receive: &ReceiveConfig{
			Store: NewMemoryStore(),
			DefaultCourier: &url.URL{
				Scheme: "authmailbox+universerpc",
				Host:   ":10029",
			},
		},
		errText: "host unspecified",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			keys := newTestKeyRing(1)
			_, err := NewClient(&Config{
				Network: tapsdk.NetworkRegtest,
				Store:   NewMemoryStore(),
				KeyRing: NewLocalKeyRing(keys),
				Signer:  NewKeyRingSigner(keys),
				Chain:   newTestChain(t),
				Receive: tc.receive,
			})
			require.ErrorContains(t, err, tc.errText)
		})
	}
}
