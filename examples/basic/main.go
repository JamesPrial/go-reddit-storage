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
		UserAgent:    "my-archiver/1.0",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Create archiver
	archiver := storage.NewArchiver(client, store)

	// Archive subreddit
	opts := storage.ArchiveOptions{
		Sort:            "hot",
		Limit:           100,
		IncludeComments: true,
		MaxCommentDepth: 10,
	}

	log.Println("Starting archive of r/golang...")
	if err := archiver.ArchiveSubreddit(ctx, "golang", opts); err != nil {
		log.Fatal(err)
	}

	// Query stored data
	queryOpts := storage.QueryOptions{
		Limit:  10,
		SortBy: "score",
	}

	posts, err := store.GetPostsBySubreddit(ctx, "golang", queryOpts)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("\nTop archived posts:")
	for i, post := range posts {
		log.Printf("%d. %s (score: %d, comments: %d)", i+1, post.Title, post.Score, post.NumComments)
	}
}