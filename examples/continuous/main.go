package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	graw "github.com/jamesprial/go-reddit-api-wrapper"
	"github.com/jamesprial/go-reddit-storage"
	"github.com/jamesprial/go-reddit-storage/sqlite"
)

func main() {
	// Setup SQLite storage
	store, err := sqlite.New("./reddit_continuous.db")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Run migrations
	ctx := context.Background()
	if err := store.RunMigrations(ctx); err != nil {
		log.Fatal(err)
	}

	// Setup Reddit client
	client, err := graw.NewClient(&graw.Config{
		ClientID:     os.Getenv("REDDIT_CLIENT_ID"),
		ClientSecret: os.Getenv("REDDIT_CLIENT_SECRET"),
		UserAgent:    "continuous-archiver/1.0",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Create archiver
	archiver := storage.NewArchiver(client, store)

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle shutdown gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("\nReceived shutdown signal, stopping...")
		cancel()
	}()

	// Start continuous monitoring
	log.Println("Starting continuous monitoring of r/golang (every 5 minutes)...")
	log.Println("Press Ctrl+C to stop")

	if err := archiver.ContinuousArchive(ctx, "golang", 5*time.Minute); err != nil {
		if err != context.Canceled {
			log.Fatal(err)
		}
	}

	log.Println("Archiver stopped successfully")
}