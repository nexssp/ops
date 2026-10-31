// Command local-nats starts an isolated JetStream broker for the local Agent
// Studio demonstration. It is deliberately not a production broker launcher.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

func main() {
	storeDir := os.Getenv("NATS_STORE_DIR")
	if storeDir == "" {
		storeDir = filepath.Join(os.TempDir(), "agent-studio-jetstream")
	}
	opts := &server.Options{
		Host:      "127.0.0.1",
		Port:      4222,
		NoSigs:    true,
		JetStream: true,
		StoreDir:  storeDir,
	}
	broker, err := server.NewServer(opts)
	if err != nil {
		log.Fatalf("create local JetStream broker: %v", err)
	}
	go broker.Start()
	if !broker.ReadyForConnections(5 * time.Second) {
		log.Fatal("local JetStream broker did not become ready")
	}
	fmt.Printf("local JetStream ready at nats://%s (storage %s)\n", broker.Addr(), storeDir)
	fmt.Println("development helper only; stop with Ctrl+C")

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
	broker.Shutdown()
}
