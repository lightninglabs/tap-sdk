package main

import (
	"log"
	"net/http"

	tapsdk "github.com/lightninglabs/tap-sdk"
)

func main() {
	cfg := loadConfig()

	client, err := newTapClient(cfg)
	if err != nil {
		log.Fatalf("connect tapd: %v", err)
	}
	defer client.Close()

	wallet := tapsdk.NewWallet(client, cfg.network)
	api := &apiServer{
		cfg:         cfg,
		coordinator: newCoordinator(wallet.NewIssuer()),
	}

	log.Printf("remote signing coordinator listening on http://%s",
		cfg.listen)
	log.Fatal(http.ListenAndServe(cfg.listen, withCORS(api)))
}
