package embedded

import (
	"testing"
	"time"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/stretchr/testify/require"
)

// TestMemoryStoreAddresses keeps one address per script key, in the order
// of insertion.
func TestMemoryStoreAddresses(t *testing.T) {
	store := NewMemoryStore()
	ctx := t.Context()

	first := AddressRecord{ScriptKey: [33]byte{2, 1}, Encoded: "first"}
	second := AddressRecord{ScriptKey: [33]byte{2, 2}, Encoded: "second"}
	require.NoError(t, store.InsertAddress(ctx, first))
	require.NoError(t, store.InsertAddress(ctx, second))

	duplicate := AddressRecord{ScriptKey: first.ScriptKey, Encoded: "other"}
	require.ErrorIs(
		t, store.InsertAddress(ctx, duplicate), ErrAddressExists,
	)

	records, err := store.ListAddresses(ctx)
	require.NoError(t, err)
	require.Equal(t, []AddressRecord{first, second}, records)
}

// TestMemoryStoreReceives keeps one receive per outpoint, address and
// announcement, and stores the proofs of an update with it.
func TestMemoryStoreReceives(t *testing.T) {
	store := NewMemoryStore()
	ctx := t.Context()

	var (
		addrA  = [33]byte{2, 1}
		addrB  = [33]byte{2, 2}
		op     = wire.OutPoint{Hash: chainhash.Hash{1}, Index: 1}
		first  = time.Unix(1_700_000_000, 0)
		detect = ReceiveDetected
		failed = ReceiveFailed
	)
	insert := func(receive ReceiveRecord) {
		t.Helper()

		got, err := store.InsertReceive(ctx, receive)
		require.NoError(t, err)
		require.Equal(t, &receive, got)
	}

	toA := ReceiveRecord{
		Outpoint:  op,
		Address:   addrA,
		CreatedAt: first,
		Payload:   []byte("a"),
	}
	toB := toA
	toB.Address = addrB
	other := toA
	other.Outpoint.Index = 2
	again := toA
	again.Announcement = [32]byte{1}
	insert(toA)
	insert(toB)
	insert(other)
	insert(again)

	// A second insert returns the record as it is stored.
	replay := toA
	replay.State = ReceiveFailed
	replay.Payload = []byte("replay")
	got, err := store.InsertReceive(ctx, replay)
	require.NoError(t, err)
	require.Equal(t, &toA, got)

	unknown := toA
	unknown.Outpoint.Index = 3
	require.ErrorIs(
		t, store.UpdateReceive(ctx, unknown, nil), ErrNotFound,
	)

	locator := ProofLocator{AssetID: [32]byte{7}, Outpoint: op}
	confirmed := toB
	confirmed.State = ReceiveConfirmed
	confirmed.BlockHash = chainhash.Hash{9}
	confirmed.BlockHeight = 21
	confirmed.Payload = []byte("b")
	confirmed.CreatedAt = first.Add(time.Hour)
	require.NoError(t, store.UpdateReceive(ctx, confirmed, []AssetProof{{
		ProofLocator: locator,
		ProofFile:    []byte("file"),
		Owned:        true,
	}}))
	confirmed.CreatedAt = first

	owned, err := store.ListOwnedProofs(ctx, false)
	require.NoError(t, err)
	require.Len(t, owned, 1)
	require.Equal(t, locator, owned[0].ProofLocator)

	failedA := toA
	failedA.State = ReceiveFailed
	require.NoError(t, store.UpdateReceive(ctx, failedA, nil))

	tests := []struct {
		name   string
		filter ReceiveFilter
		want   []ReceiveRecord
	}{{
		name: "all",
		want: []ReceiveRecord{failedA, confirmed, other, again},
	}, {
		name:   "address",
		filter: ReceiveFilter{Address: &addrA},
		want:   []ReceiveRecord{failedA, other, again},
	}, {
		name:   "state",
		filter: ReceiveFilter{State: &detect},
		want:   []ReceiveRecord{other, again},
	}, {
		name:   "address and state",
		filter: ReceiveFilter{Address: &addrA, State: &failed},
		want:   []ReceiveRecord{failedA},
	}, {
		name:   "outpoint",
		filter: ReceiveFilter{Outpoint: &op},
		want:   []ReceiveRecord{failedA, confirmed, again},
	}, {
		name:   "outpoint and address",
		filter: ReceiveFilter{Outpoint: &op, Address: &addrB},
		want:   []ReceiveRecord{confirmed},
	}, {
		name:   "no match",
		filter: ReceiveFilter{Address: &addrB, State: &detect},
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			receives, err := store.ListReceives(ctx, tc.filter)
			require.NoError(t, err)
			require.Equal(t, tc.want, receives)
		})
	}
}
