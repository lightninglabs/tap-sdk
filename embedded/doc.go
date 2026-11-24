// Package embedded is an in-process tap-sdk transport. Where the grpc and rest
// packages forward each call to a tapd, this package answers the calls inside
// the host process: it verifies proofs, derives asset keys, signs and seals
// asset transfers, publishes their anchor transactions and writes the final
// proofs once they confirm.
//
// Every asset rule is applied by tapd's own packages. The host supplies what a
// tapd would take from lnd and its database: a key ring, a signer, a chain
// source and a store.
//
// The engine covers the calls a wallet needs to hold, receive and move assets
// with caller-funded anchor transactions. With a ReceiveConfig it also
// creates, decodes and lists version 2 addresses, and receives the transfers
// sent to them: it reads the auth mailbox of each address on the address's
// proof courier, and fetches and verifies the proofs of each transfer a
// sender announces there. Minting, universe queries, address sends and event
// streams return codes.Unimplemented.
package embedded
