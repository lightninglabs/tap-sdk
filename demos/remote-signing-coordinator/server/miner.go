package main

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type blockMiner interface {
	MineBlocks(context.Context, int) error
}

type regtestMiner struct {
	container string
	user      string
	password  string
	wallet    string
}

func newBlockMiner(cfg config) blockMiner {
	if !cfg.miningEnabled() {
		return nil
	}

	return regtestMiner{
		container: cfg.bitcoindContainer,
		user:      cfg.bitcoindRPCUser,
		password:  cfg.bitcoindRPCPass,
		wallet:    cfg.regtestMinerWallet,
	}
}

func (m regtestMiner) MineBlocks(ctx context.Context, blocks int) error {
	if blocks <= 0 {
		return nil
	}

	if err := m.ensureWallet(ctx); err != nil {
		return err
	}

	address, err := m.bitcoinCLI(
		ctx, "-rpcwallet="+m.wallet, "getnewaddress", "", "bech32",
	)
	if err != nil {
		return err
	}

	_, err = m.bitcoinCLI(
		ctx, "-rpcwallet="+m.wallet, "generatetoaddress",
		strconv.Itoa(blocks), strings.TrimSpace(address),
	)
	if err != nil {
		return err
	}

	return nil
}

func (m regtestMiner) ensureWallet(ctx context.Context) error {
	_, createErr := m.bitcoinCLI(ctx, "createwallet", m.wallet)
	if createErr == nil {
		return nil
	}

	_, loadErr := m.bitcoinCLI(ctx, "loadwallet", m.wallet)
	if loadErr == nil || strings.Contains(loadErr.Error(), "already loaded") {
		return nil
	}

	return fmt.Errorf("create/load regtest miner wallet: %w", createErr)
}

func (m regtestMiner) bitcoinCLI(ctx context.Context,
	args ...string) (string, error) {

	baseArgs := []string{
		"exec",
		m.container,
		"bitcoin-cli",
		"-regtest",
		"-rpcuser=" + m.user,
		"-rpcpassword=" + m.password,
	}
	//nolint:gosec // Demo intentionally shells out to local Docker.
	cmd := exec.CommandContext(ctx, "docker", append(baseArgs, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf(
			"bitcoin-cli %s: %w: %s", strings.Join(args, " "),
			err, strings.TrimSpace(string(output)),
		)
	}

	return strings.TrimSpace(string(output)), nil
}
