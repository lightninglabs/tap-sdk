package embedded

import (
	"testing"

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
