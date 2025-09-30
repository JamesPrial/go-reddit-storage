package main

import (
	"context"
	"log"
	"os"

	graw "github.com/jamesprial/go-reddit-api-wrapper"
	"github.com/jamesprial/go-reddit-storage"
	"github.com/jamesprial/go-reddit-storage/postgres"
)

func main() {
	// Setup storage
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://user:pass@localhost/reddit?sslmode=disable"
	}

	store, err := postgres.New(dbURL)
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
		UserAgent:    "backfill-archiver/1.0",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Create archiver
	archiver := storage.NewArchiver(client, store)

	// Backfill historical posts
	subreddit := "golang"
	maxPosts := 1000
	includeComments := true

	log.Printf("Starting backfill of r/%s (up to %d posts)...", subreddit, maxPosts)
	log.Println("This may take a while depending on Reddit's API rate limits...")

	if err := archiver.BackfillSubreddit(ctx, subreddit, maxPosts, includeComments); err != nil {
		log.Fatal(err)
	}

	log.Println("Backfill completed successfully!")

	// Show statistics
	queryOpts := storage.QueryOptions{
		Limit:     1,
		SortBy:    "created",
		SortOrder: "asc",
	}

	posts, err := store.GetPostsBySubreddit(ctx, subreddit, queryOpts)
	if err == nil && len(posts) > 0 {
		log.Printf("Oldest post: %s (created: %v)", posts[0].Title, posts[0].CreatedUTC)
	}

	queryOpts.SortOrder = "desc"
	posts, err = store.GetPostsBySubreddit(ctx, subreddit, queryOpts)
	if err == nil && len(posts) > 0 {
		log.Printf("Newest post: %s (created: %v)", posts[0].Title, posts[0].CreatedUTC)
	}
}