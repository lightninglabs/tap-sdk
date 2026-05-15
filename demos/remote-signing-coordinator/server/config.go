package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tapsdk "github.com/lightninglabs/tap-sdk"
)

type config struct {
	demoDir      string
	listen       string
	transport    string
	tapdHost     string
	tapdBaseURL  string
	network      tapsdk.Network
	tlsPath      string
	macaroonPath string
	tlsInsecure  bool

	regtestAutoMine    bool
	regtestMineBlocks  int
	bitcoindContainer  string
	bitcoindRPCUser    string
	bitcoindRPCPass    string
	regtestMinerWallet string
}

func loadConfig() config {
	demoDir := findDemoDir()
	values := readEnvFiles(
		filepath.Join(demoDir, ".env.example"),
		filepath.Join(demoDir, ".env"),
	)

	var network string
	cfg := config{
		demoDir: demoDir,
	}

	flag.StringVar(&cfg.listen, "listen", configValue(
		values, "COORDINATOR_LISTEN", "127.0.0.1:8091",
	), "HTTP listen address")
	flag.StringVar(&cfg.transport, "transport", configValue(
		values, "TAP_TRANSPORT", "grpc",
	), "tapd transport: grpc or rest")
	flag.StringVar(&cfg.tapdHost, "tapd-host", configValue(
		values, "TAPD_HOST", "localhost:10029",
	), "tapd gRPC host")
	flag.StringVar(&cfg.tapdBaseURL, "tapd-rest-url", configValue(
		values, "TAPD_REST_URL", "https://localhost:8089",
	), "tapd REST base URL")
	flag.StringVar(&network, "network", configValue(
		values, "TAPD_NETWORK", "regtest",
	), "Bitcoin network")
	flag.StringVar(&cfg.tlsPath, "tls-cert", configValue(
		values, "TAPD_TLS_PATH", ".tapd/alice/tls.cert",
	), "tapd TLS certificate path")
	flag.StringVar(&cfg.macaroonPath, "macaroon", configValue(
		values, "TAPD_MACAROON_PATH", ".tapd/alice/admin.macaroon",
	), "tapd macaroon path")
	flag.BoolVar(&cfg.tlsInsecure, "tls-insecure", configBool(
		values, "TAPD_TLS_INSECURE",
	), "disable TLS verification")
	flag.BoolVar(&cfg.regtestAutoMine, "regtest-auto-mine", configBoolDefault(
		values, "REGTEST_AUTO_MINE", true,
	), "mine regtest blocks after an Issuance is broadcast")
	flag.IntVar(&cfg.regtestMineBlocks, "regtest-mine-blocks", configInt(
		values, "REGTEST_MINE_BLOCKS", 6,
	), "number of regtest blocks to mine after an Issuance")
	flag.StringVar(&cfg.bitcoindContainer, "bitcoind-container", configValue(
		values, "BITCOIND_CONTAINER", "tap-sdk-bitcoind",
	), "regtest bitcoind Docker container")
	flag.StringVar(&cfg.bitcoindRPCUser, "bitcoind-rpc-user", configValue(
		values, "BITCOIND_USER", "devuser",
	), "regtest bitcoind RPC user")
	flag.StringVar(&cfg.bitcoindRPCPass, "bitcoind-rpc-pass", configValue(
		values, "BITCOIND_PASS", "devpass",
	), "regtest bitcoind RPC password")
	flag.StringVar(&cfg.regtestMinerWallet, "regtest-miner-wallet",
		configValue(values, "REGTEST_MINER_WALLET", "miner"),
		"regtest bitcoind wallet used for mining")
	flag.Parse()

	parsedNetwork, err := parseNetwork(network)
	if err != nil {
		log.Fatal(err)
	}
	cfg.network = parsedNetwork
	cfg.tlsPath = resolveDemoPath(demoDir, cfg.tlsPath)
	cfg.macaroonPath = resolveDemoPath(demoDir, cfg.macaroonPath)
	if cfg.regtestMineBlocks < 0 {
		log.Fatal("REGTEST_MINE_BLOCKS must be zero or greater")
	}

	return cfg
}

func (c config) miningEnabled() bool {
	return c.network == tapsdk.NetworkRegtest &&
		c.regtestAutoMine &&
		c.regtestMineBlocks > 0
}

func parseNetwork(network string) (tapsdk.Network, error) {
	switch tapsdk.Network(strings.ToLower(network)) {
	case tapsdk.NetworkMainnet:
		return tapsdk.NetworkMainnet, nil
	case tapsdk.NetworkTestnet:
		return tapsdk.NetworkTestnet, nil
	case tapsdk.NetworkTestnet4:
		return tapsdk.NetworkTestnet4, nil
	case tapsdk.NetworkRegtest:
		return tapsdk.NetworkRegtest, nil
	case tapsdk.NetworkSimnet:
		return tapsdk.NetworkSimnet, nil
	case tapsdk.NetworkSignet:
		return tapsdk.NetworkSignet, nil
	default:
		return "", fmt.Errorf("unsupported network: %s", network)
	}
}

func findDemoDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	for dir := cwd; ; dir = filepath.Dir(dir) {
		if fileExists(filepath.Join(dir, ".env.example")) &&
			filepath.Base(dir) == "remote-signing-coordinator" {

			return dir
		}

		candidate := filepath.Join(
			dir, "demos", "remote-signing-coordinator",
			".env.example",
		)
		if fileExists(candidate) {
			return filepath.Dir(candidate)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
	}
}

func readEnvFiles(paths ...string) map[string]string {
	values := make(map[string]string)
	for _, path := range paths {
		file, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			log.Fatalf("read %s: %v", path, err)
		}

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			value = strings.Trim(value, `"'`)
			if key != "" {
				values[key] = value
			}
		}
		if err := scanner.Err(); err != nil {
			log.Fatalf("read %s: %v", path, err)
		}
		_ = file.Close()
	}

	return values
}

func configValue(values map[string]string, key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	if value := values[key]; value != "" {
		return value
	}

	return fallback
}

func configBool(values map[string]string, key string) bool {
	return configBoolDefault(values, key, false)
}

func configBoolDefault(values map[string]string, key string,
	fallback bool) bool {

	switch strings.ToLower(configValue(values, key, "")) {
	case "1", "true", "yes", "y":
		return true
	case "0", "false", "no", "n":
		return false
	default:
		return fallback
	}
}

func configInt(values map[string]string, key string, fallback int) int {
	value := configValue(values, key, "")
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		log.Fatalf("%s must be an integer: %v", key, err)
	}

	return parsed
}

func resolveDemoPath(demoDir string, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(demoDir, path)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}
