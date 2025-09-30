# Go Reddit Storage

Database persistence layer for Reddit data fetched via [go-reddit-api-wrapper](https://github.com/jamesprial/go-reddit-api-wrapper).

## Features

- **Multiple Storage Backends**: PostgreSQL and SQLite support with identical interfaces
- **Idempotent Operations**: Safe to re-archive same content without duplicates
- **Comment Threading**: Preserves Reddit's nested comment structure
- **Bulk Operations**: Efficient batch inserts for high-performance archiving
- **High-Level Archiving**: Simple APIs for common archiving workflows
- **Automatic Migrations**: Database schema migrations run automatically
- **Full-Text Search**: Search archived posts and comments (PostgreSQL)
- **CLI Tool**: Command-line utility for quick archiving tasks

## Installation

```bash
go get github.com/jamesprial/go-reddit-storage
```

## Quick Start

### Basic Usage

```go
package main

import (
    "context"
    "log"

    graw "github.com/jamesprial/go-reddit-api-wrapper"
    "github.com/jamesprial/go-reddit-storage"
    "github.com/jamesprial/go-reddit-storage/sqlite"
)

func main() {
    // Initialize storage
    store, err := sqlite.New("./reddit.db")
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()

    // Run migrations
    ctx := context.Background()
    if err := store.RunMigrations(ctx); err != nil {
        log.Fatal(err)
    }

    // Initialize Reddit client
    client, err := graw.NewClient(&graw.Config{
        ClientID:     "your-client-id",
        ClientSecret: "your-client-secret",
        UserAgent:    "my-archiver/1.0",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Create archiver and archive a subreddit
    archiver := storage.NewArchiver(client, store)
    opts := storage.ArchiveOptions{
        Sort:            "hot",
        Limit:           100,
        IncludeComments: true,
    }

    if err := archiver.ArchiveSubreddit(ctx, "golang", opts); err != nil {
        log.Fatal(err)
    }

    log.Println("Archive complete!")
}
```

## Storage Backends

### SQLite

Best for:
- Single-machine deployments
- Development and testing
- Small to medium archives (< 10M posts)
- Embedded applications

```go
import "github.com/jamesprial/go-reddit-storage/sqlite"

store, err := sqlite.New("./reddit.db")
```

### PostgreSQL

Best for:
- Production deployments
- Large-scale archiving
- Full-text search requirements
- Multi-user access

```go
import "github.com/jamesprial/go-reddit-storage/postgres"

store, err := postgres.New("postgres://user:pass@localhost/reddit?sslmode=disable")
```

## Core API

### Storage Interface

```go
type Storage interface {
    // Posts
    SavePost(ctx context.Context, post *types.Post) error
    SavePosts(ctx context.Context, posts []*types.Post) error
    GetPost(ctx context.Context, id string) (*types.Post, error)
    GetPostsBySubreddit(ctx context.Context, subreddit string, opts QueryOptions) ([]*types.Post, error)

    // Comments
    SaveComment(ctx context.Context, comment *types.Comment) error
    SaveComments(ctx context.Context, comments []*types.Comment) error
    GetCommentsByPost(ctx context.Context, postID string) ([]*types.Comment, error)

    // Subreddits
    SaveSubreddit(ctx context.Context, sub *types.Subreddit) error
    GetSubreddit(ctx context.Context, name string) (*types.Subreddit, error)

    // Queries
    SearchPosts(ctx context.Context, query string, opts QueryOptions) ([]*types.Post, error)
    GetPostStats(ctx context.Context, postID string) (*PostStats, error)

    // Management
    RunMigrations(ctx context.Context) error
    Close() error
}
```

### Archiver

The `Archiver` combines a Reddit API client with a storage backend for high-level operations:

```go
// Archive a subreddit
archiver.ArchiveSubreddit(ctx, "golang", storage.ArchiveOptions{
    Sort:            "hot",
    Limit:           100,
    IncludeComments: true,
})

// Archive a specific post
archiver.ArchivePost(ctx, "golang", "abc123", true)

// Continuous monitoring (runs until context is cancelled)
archiver.ContinuousArchive(ctx, "golang", 5*time.Minute)

// Backfill historical posts
archiver.BackfillSubreddit(ctx, "golang", 1000, true)

// Update scores for recent posts
archiver.UpdateScores(ctx, "golang", 24*time.Hour)
```

## Query Options

```go
opts := storage.QueryOptions{
    Limit:     100,           // Max results
    Offset:    0,             // Pagination offset
    SortBy:    "score",       // "created", "score", "comments"
    SortOrder: "desc",        // "asc", "desc"
    StartDate: time.Now().Add(-7 * 24 * time.Hour),
    EndDate:   time.Now(),
}

posts, err := store.GetPostsBySubreddit(ctx, "golang", opts)
```

## CLI Tool

### Installation

```bash
go install github.com/jamesprial/go-reddit-storage/cmd/reddit-archiver@latest
```

### Usage

```bash
# Set environment variables
export REDDIT_CLIENT_ID="your-client-id"
export REDDIT_CLIENT_SECRET="your-client-secret"

# Archive a subreddit (SQLite)
reddit-archiver -subreddit golang -limit 100 -comments

# Use PostgreSQL
export DATABASE_URL="postgres://user:pass@localhost/reddit"
reddit-archiver -subreddit golang -db-type postgres -limit 100

# Continuous monitoring
reddit-archiver -subreddit golang -continuous -interval 5m

# Backfill historical posts
reddit-archiver -subreddit golang -backfill -max-backfill 1000
```

### CLI Flags

- `-subreddit`: Subreddit to archive (required)
- `-db-type`: Database type: `sqlite` or `postgres` (default: `sqlite`)
- `-db`: Database connection string
- `-sort`: Sort type: `hot`, `new`, `top` (default: `hot`)
- `-limit`: Number of posts to fetch (default: `25`)
- `-comments`: Include comments (default: `true`)
- `-continuous`: Continuously monitor and archive
- `-interval`: Interval for continuous archiving (default: `5m`)
- `-backfill`: Backfill historical posts
- `-max-backfill`: Maximum posts to backfill (default: `1000`)

## Database Schema

### Tables

- **subreddits**: Subreddit metadata
- **posts**: Post content and metadata
- **comments**: Comments with threading support
- **archive_metadata**: Sync state tracking
- **schema_version**: Migration tracking

### Key Features

- **Foreign Keys**: Enforced referential integrity
- **Indexes**: Optimized for common query patterns
- **Full-Text Search**: PostgreSQL GIN indexes for text search
- **Timestamps**: Track archival and update times
- **Raw JSON**: Store complete API responses for future flexibility

## Examples

See the [examples/](examples/) directory for complete examples:

- **basic**: Simple archiving workflow
- **continuous**: Continuous monitoring with graceful shutdown
- **backfill**: Historical data backfilling

## Testing

### Run All Tests

```bash
go test -v ./...
```

### Run SQLite Tests

```bash
go test -v ./sqlite
```

### Run PostgreSQL Tests

Set the `TEST_POSTGRES_URL` environment variable:

```bash
export TEST_POSTGRES_URL="postgres://user:pass@localhost/reddit_test?sslmode=disable"
go test -v ./postgres
```

### Integration Tests with Docker

```bash
# Start PostgreSQL
docker run -d --name test-postgres \
  -e POSTGRES_PASSWORD=test \
  -e POSTGRES_DB=reddit_test \
  -p 5432:5432 \
  postgres:15

# Run tests
export TEST_POSTGRES_URL="postgres://postgres:test@localhost/reddit_test?sslmode=disable"
go test -v ./...

# Cleanup
docker stop test-postgres
docker rm test-postgres
```

## Performance

### Batch Operations

Use batch operations for best performance:

```go
// Good: Batch insert
posts := []*types.Post{...}  // 1000 posts
store.SavePosts(ctx, posts)   // Single transaction

// Avoid: Individual inserts
for _, post := range posts {
    store.SavePost(ctx, post)  // 1000 transactions
}
```

### Expected Throughput

- **PostgreSQL**: 500-1000 posts/sec (batch inserts)
- **SQLite**: 200-500 posts/sec (batch inserts, WAL mode)

## Best Practices

1. **Use batch operations** for inserting multiple posts/comments
2. **Enable WAL mode** for SQLite (done automatically)
3. **Run migrations** before first use
4. **Handle context cancellation** for graceful shutdown
5. **Use connection pooling** for PostgreSQL in production
6. **Store raw JSON** for future schema evolution
7. **Regular backups** of your database

## Requirements

- Go 1.21 or later
- PostgreSQL 12+ (for PostgreSQL backend)
- Reddit API credentials ([create an app](https://www.reddit.com/prefs/apps))

## Dependencies

- [go-reddit-api-wrapper](https://github.com/jamesprial/go-reddit-api-wrapper) - Reddit API client
- [lib/pq](https://github.com/lib/pq) - PostgreSQL driver
- [modernc.org/sqlite](https://modernc.org/sqlite) - Pure Go SQLite driver

## License

MIT License - see LICENSE.md for details

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

## Related Projects

- [go-reddit-api-wrapper](https://github.com/jamesprial/go-reddit-api-wrapper) - Reddit API client library