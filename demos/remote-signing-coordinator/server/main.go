package main

import (
	"log"
	"net/http"
	"time"

	tapsdk "github.com/lightninglabs/tap-sdk"
)

func main() {
	cfg := loadConfig()

	client, err := newTapClient(cfg)
	if err != nil {
		log.Fatalf("connect tapd: %v", err)
	}

	wallet := tapsdk.NewWallet(client, cfg.network)
	api := &apiServer{
		cfg: cfg,
		coordinator: newCoordinator(
			wallet.NewIssuer(), client, newBlockMiner(cfg),
			cfg.regtestMineBlocks,
		),
	}

	log.Printf("remote signing coordinator listening on http://%s",
		cfg.listen)

	server := &http.Server{
		Addr:              cfg.listen,
		Handler:           withCORS(api),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		client.Close()
		log.Fatal(err)
	}
}
