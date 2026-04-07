package grpc

import (
	"github.com/lightninglabs/tap-sdk/entities"
	"github.com/lightninglabs/taproot-assets/taprpc/assetwalletrpc"
)

func marshalBackupMode(mode entities.BackupMode) assetwalletrpc.BackupMode {
	switch mode {
	case entities.BackupModeCompact:
		return assetwalletrpc.BackupMode_COMPACT

	case entities.BackupModeOptimistic:
		return assetwalletrpc.BackupMode_OPTIMISTIC

	default:
		return assetwalletrpc.BackupMode_RAW
	}
}
