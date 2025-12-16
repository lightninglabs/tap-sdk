package grpc

import (
	"path/filepath"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/tap-sdk/entities"
	"google.golang.org/grpc"
)

const (
	// defaultRPCTimeout is the default timeout used for rpc calls.
	defaultRPCTimeout = 30 * time.Second
)

var (
	defaultTapdDir         = btcutil.AppDataDir("tapd", false)
	defaultTLSCertFilename = "tls.cert"
	defaultTLSCertPath     = filepath.Join(
		defaultTapdDir, defaultTLSCertFilename,
	)
	defaultDataDir     = "data"
	defaultChainSubDir = "chain"

	// maxMsgRecvSize is the largest gRPC message our client will receive.
	// We set this to 800MiB.
	maxMsgRecvSize = grpc.MaxCallRecvMsgSize(800 * 1024 * 1024)
)

// Config holds all configuration settings that are needed to connect to a tapd
// instance.
type Config struct {
	// Host is the network address (host:port) of the tapd instance to connect
	// to.
	Host string

	// Network is the bitcoin network we expect the tapd instance to operate on.
	Network entities.Network

	// MacaroonDir is the directory where all tapd macaroons can be found.
	// Either this, MacaroonPath, or MacaroonHex should be set,
	// but only one of them, depending on macaroon preferences.
	MacaroonDir string

	// MacaroonPath is the full path to a custom macaroon file.
	MacaroonPath string

	// MacaroonHex is a hexadecimal encoded macaroon string. Either
	// this, MacaroonDir, or MacaroonPath should be set, but only
	// one of them.
	MacaroonHex string

	// TLSPath is the path to tapd's TLS certificate file.
	TLSPath string

	// TLSData holds the TLS certificate data. Only this or TLSPath can be
	// set, not both.
	TLSData string

	// Insecure can be checked if we don't need to use tls, such as if
	// we're connecting to tapd via a bufconn, then we'll skip verification.
	Insecure bool

	// SystemCert specifies whether we'll fallback to a system cert pool
	// for tls.
	SystemCert bool

	// RPCTimeout is an optional custom timeout that will be used for rpc
	// calls to tapd. If this value is not set, it will default to 30
	// seconds.
	RPCTimeout time.Duration
}
