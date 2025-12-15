package codec

import (
	"bytes"
	"fmt"
	"io"
)

const maxAltLeavesSize = 65535

// EncodeAltLeaves serializes a list of alt leaves using the minimal format
// expected by tapd: varint leaf count followed by length-prefixed raw leaf
// bytes.
func EncodeAltLeaves(leaves [][]byte) ([]byte, error) {
	var (
		buf     bytes.Buffer
		scratch [9]byte
	)

	if err := WriteVarInt(&buf, uint64(len(leaves)), scratch[:]); err != nil {
		return nil, err
	}

	for _, leaf := range leaves {
		if err := WriteVarInt(&buf, uint64(len(leaf)), scratch[:]); err != nil {
			return nil, err
		}

		if _, err := buf.Write(leaf); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

// DecodeAltLeaves decodes a set of alt leaves from the minimal format used by
// tapd: varint leaf count followed by length-prefixed raw leaf bytes.
func DecodeAltLeaves(encoded []byte) ([][]byte, error) {
	if len(encoded) == 0 {
		return nil, nil
	}

	// The total size of all alt leaves is capped by the protocol.
	if len(encoded) > maxAltLeavesSize {
		return nil, fmt.Errorf("alt leaves payload too large: %d", len(encoded))
	}

	r := bytes.NewReader(encoded)
	var scratch [8]byte

	numLeaves, err := ReadVarInt(r, scratch[:])
	if err != nil {
		return nil, fmt.Errorf("read alt leaves count: %w", err)
	}

	if numLeaves == 0 {
		if r.Len() != 0 {
			return nil, fmt.Errorf("alt leaves has trailing bytes")
		}
		return nil, nil
	}

	// Each leaf requires at least a 1-byte length varint, so numLeaves cannot
	// exceed the remaining bytes.
	if numLeaves > uint64(r.Len()) {
		return nil, fmt.Errorf("alt leaves count %d exceeds remaining bytes %d",
			numLeaves, r.Len())
	}

	leaves := make([][]byte, 0, numLeaves)
	for i := uint64(0); i < numLeaves; i++ {
		leafLen, err := ReadVarInt(r, scratch[:])
		if err != nil {
			return nil, fmt.Errorf("read alt leaf length: %w", err)
		}

		if leafLen > uint64(r.Len()) {
			return nil, fmt.Errorf("alt leaf length %d exceeds remaining bytes %d",
				leafLen, r.Len())
		}

		leaf := make([]byte, leafLen)
		if _, err := io.ReadFull(r, leaf); err != nil {
			return nil, fmt.Errorf("read alt leaf bytes: %w", err)
		}

		leaves = append(leaves, leaf)
	}

	if r.Len() != 0 {
		return nil, fmt.Errorf("alt leaves has trailing bytes")
	}

	return leaves, nil
}
