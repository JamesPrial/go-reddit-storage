package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	graw "github.com/jamesprial/go-reddit-api-wrapper"
	"github.com/jamesprial/go-reddit-storage"
	"github.com/jamesprial/go-reddit-storage/postgres"
	"github.com/jamesprial/go-reddit-storage/sqlite"
)

func main() {
	var (
		subreddit   = flag.String("subreddit", "", "Subreddit to archive (required)")
		dbType      = flag.String("db-type", "sqlite", "Database type: sqlite or postgres")
		dbURL       = flag.String("db", "", "Database connection string")
		sort        = flag.String("sort", "hot", "Sort: hot, new, top")
		limit       = flag.Int("limit", 25, "Number of posts")
		comments    = flag.Bool("comments", true, "Include comments")
		continuous  = flag.Bool("continuous", false, "Continuously monitor and archive")
		interval    = flag.Duration("interval", 5*time.Minute, "Interval for continuous archiving")
		backfill    = flag.Bool("backfill", false, "Backfill historical posts")
		maxBackfill = flag.Int("max-backfill", 1000, "Maximum posts to backfill")
	)
	flag.Parse()

	// Validate required flags
	if *subreddit == "" {
		log.Fatal("Error: -subreddit flag is required")
	}

	// Setup database connection string
	connString := *dbURL
	if connString == "" {
		switch *dbType {
		case "sqlite":
			connString = "./reddit.db"
		case "postgres":
			connString = os.Getenv("DATABASE_URL")
			if connString == "" {
				log.Fatal("Error: -db flag or DATABASE_URL environment variable required for postgres")
			}
		default:
			log.Fatalf("Error: unsupported database type: %s", *dbType)
		}
	}

	// Initialize storage
	var store storage.Storage
	var err error

	switch strings.ToLower(*dbType) {
	case "sqlite":
		store, err = sqlite.New(connString)
	case "postgres", "postgresql":
		store, err = postgres.New(connString)
	default:
		log.Fatalf("Error: unsupported database type: %s", *dbType)
	}

	if err != nil {
		log.Fatalf("Error initializing storage: %v", err)
	}
	defer store.Close()

	// Run migrations
	ctx := context.Background()
	if err := store.RunMigrations(ctx); err != nil {
		log.Fatalf("Error running migrations: %v", err)
	}

	// Initialize Reddit client
	clientID := os.Getenv("REDDIT_CLIENT_ID")
	clientSecret := os.Getenv("REDDIT_CLIENT_SECRET")
	userAgent := os.Getenv("REDDIT_USER_AGENT")

	if clientID == "" || clientSecret == "" {
		log.Fatal("Error: REDDIT_CLIENT_ID and REDDIT_CLIENT_SECRET environment variables are required")
	}

	if userAgent == "" {
		userAgent = "reddit-archiver/1.0"
	}

	client, err := graw.NewClient(&graw.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		UserAgent:    userAgent,
	})

	if err != nil {
		log.Fatalf("Error initializing Reddit client: %v", err)
	}

	// Create archiver
	archiver := storage.NewArchiver(client, store)

	// Execute based on mode
	if *backfill {
		log.Printf("Starting backfill of r/%s (max %d posts)...", *subreddit, *maxBackfill)
		if err := archiver.BackfillSubreddit(ctx, *subreddit, *maxBackfill, *comments); err != nil {
			log.Fatalf("Error during backfill: %v", err)
		}
		log.Printf("Backfill completed successfully")
	} else if *continuous {
		log.Printf("Starting continuous archiving of r/%s (interval: %s)...", *subreddit, *interval)
		if err := archiver.ContinuousArchive(ctx, *subreddit, *interval); err != nil {
			log.Fatalf("Error during continuous archive: %v", err)
		}
	} else {
		// One-time archive
		opts := storage.ArchiveOptions{
			Sort:            *sort,
			Limit:           *limit,
			IncludeComments: *comments,
		}

		log.Printf("Archiving r/%s (sort: %s, limit: %d, comments: %v)...",
			*subreddit, *sort, *limit, *comments)

		if err := archiver.ArchiveSubreddit(ctx, *subreddit, opts); err != nil {
			log.Fatalf("Error during archive: %v", err)
		}

		log.Printf("Successfully archived r/%s", *subreddit)

		// Show some stats
		stats, err := store.GetPostStats(ctx, "")
		if err == nil && stats != nil {
			log.Printf("Total comments: %d, Max depth: %d", stats.CommentCount, stats.MaxCommentDepth)
		}
	}
}