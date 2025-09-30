# Go Reddit Storage - Implementation Specification

This document provides complete specifications for building `go-reddit-storage`, a companion library to `go-reddit-api-wrapper` that provides database persistence for Reddit data.

## Project Overview

Create a Go library that provides storage backends (PostgreSQL, SQLite) for persisting Reddit data fetched via `go-reddit-api-wrapper`. The library should include high-level archiving utilities that combine fetching and storage operations.

**Repository:** `go-reddit-storage`
**Module Path:** `github.com/jamesprial/go-reddit-storage`
**Dependencies:** `github.com/jamesprial/go-reddit-api-wrapper@v0.1.0`

## Architecture Goals

1. **Pluggable Storage** - Interface-based design allowing multiple backends
2. **Idempotent Operations** - Safe to re-archive same content
3. **Efficient Bulk Operations** - Batch inserts for performance
4. **Comment Threading** - Preserve Reddit's nested structure
5. **Incremental Sync** - Track what's been archived, support updates
6. **Type Safety** - Leverage `types` from the wrapper library
7. **Transaction Support** - Atomic operations where needed

## Package Structure

```
go-reddit-storage/
├── go.mod
├── go.sum
├── README.md
├── LICENSE.md
├── CLAUDE.md                       # Instructions for Claude
├── storage.go                      # Core interfaces and types
├── archiver.go                     # High-level archiving logic
├── schema/
│   ├── schema.go                   # Schema version management
│   └── migrations/
│       ├── postgres/
│       │   ├── 001_initial.sql
│       │   └── 002_indexes.sql
│       └── sqlite/
│           ├── 001_initial.sql
│           └── 002_indexes.sql
├── postgres/
│   ├── postgres.go                 # PostgreSQL implementation
│   ├── posts.go                    # Post-specific queries
│   ├── comments.go                 # Comment-specific queries
│   └── postgres_test.go
├── sqlite/
│   ├── sqlite.go                   # SQLite implementation
│   ├── posts.go
│   ├── comments.go
│   └── sqlite_test.go
├── internal/
│   └── testutil/
│       └── fixtures.go             # Test data helpers
├── examples/
│   ├── basic/
│   │   └── main.go                 # Simple archiving example
│   ├── continuous/
│   │   └── main.go                 # Continuous monitoring
│   └── backfill/
│       └── main.go                 # Historical data backfill
└── cmd/
    └── reddit-archiver/
        └── main.go                 # CLI tool
```

## Core Interfaces (storage.go)

```go
package storage

import (
    "context"
    "time"

    "github.com/jamesprial/go-reddit-api-wrapper/pkg/types"
)

// Storage is the main interface for persisting Reddit data
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

// QueryOptions provides filtering and pagination for queries
type QueryOptions struct {
    Limit      int
    Offset     int
    SortBy     string    // "created", "score", "comments"
    SortOrder  string    // "asc", "desc"
    StartDate  time.Time
    EndDate    time.Time
}

// PostStats aggregates statistics about a post
type PostStats struct {
    PostID          string
    CommentCount    int
    MaxCommentDepth int
    LastUpdated     time.Time
}
```

## Database Schema

### PostgreSQL Schema (migrations/postgres/001_initial.sql)

```sql
-- Subreddits table
CREATE TABLE IF NOT EXISTS subreddits (
    name TEXT PRIMARY KEY,
    display_name TEXT,
    title TEXT,
    description TEXT,
    subscribers INTEGER,
    created_utc TIMESTAMP,
    last_synced TIMESTAMP DEFAULT NOW(),
    raw_json JSONB
);

-- Posts table
CREATE TABLE IF NOT EXISTS posts (
    id TEXT PRIMARY KEY,
    subreddit TEXT NOT NULL REFERENCES subreddits(name),
    author TEXT,
    title TEXT NOT NULL,
    selftext TEXT,
    url TEXT,
    score INTEGER DEFAULT 0,
    upvote_ratio REAL,
    num_comments INTEGER DEFAULT 0,
    created_utc TIMESTAMP NOT NULL,
    edited_utc TIMESTAMP,
    is_self BOOLEAN DEFAULT false,
    is_video BOOLEAN DEFAULT false,
    archived_at TIMESTAMP DEFAULT NOW(),
    last_updated TIMESTAMP DEFAULT NOW(),
    raw_json JSONB,
    CONSTRAINT posts_subreddit_fkey FOREIGN KEY (subreddit)
        REFERENCES subreddits(name) ON DELETE CASCADE
);

-- Comments table
CREATE TABLE IF NOT EXISTS comments (
    id TEXT PRIMARY KEY,
    post_id TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    parent_id TEXT,
    author TEXT,
    body TEXT,
    score INTEGER DEFAULT 0,
    depth INTEGER DEFAULT 0,
    created_utc TIMESTAMP NOT NULL,
    edited_utc TIMESTAMP,
    archived_at TIMESTAMP DEFAULT NOW(),
    last_updated TIMESTAMP DEFAULT NOW(),
    raw_json JSONB,
    CONSTRAINT comments_parent_fkey FOREIGN KEY (parent_id)
        REFERENCES comments(id) ON DELETE CASCADE
);

-- Archive metadata for tracking sync state
CREATE TABLE IF NOT EXISTS archive_metadata (
    subreddit TEXT PRIMARY KEY,
    last_post_id TEXT,
    last_sync TIMESTAMP,
    total_posts INTEGER DEFAULT 0,
    total_comments INTEGER DEFAULT 0
);
```

### Indexes (migrations/postgres/002_indexes.sql)

```sql
-- Posts indexes
CREATE INDEX IF NOT EXISTS idx_posts_subreddit ON posts(subreddit);
CREATE INDEX IF NOT EXISTS idx_posts_created ON posts(created_utc DESC);
CREATE INDEX IF NOT EXISTS idx_posts_score ON posts(score DESC);
CREATE INDEX IF NOT EXISTS idx_posts_author ON posts(author);

-- Comments indexes
CREATE INDEX IF NOT EXISTS idx_comments_post_id ON comments(post_id);
CREATE INDEX IF NOT EXISTS idx_comments_parent_id ON comments(parent_id);
CREATE INDEX IF NOT EXISTS idx_comments_created ON comments(created_utc DESC);
CREATE INDEX IF NOT EXISTS idx_comments_author ON comments(author);

-- Full-text search (PostgreSQL specific)
CREATE INDEX IF NOT EXISTS idx_posts_title_search ON posts USING GIN(to_tsvector('english', title));
CREATE INDEX IF NOT EXISTS idx_posts_selftext_search ON posts USING GIN(to_tsvector('english', selftext));
CREATE INDEX IF NOT EXISTS idx_comments_body_search ON comments USING GIN(to_tsvector('english', body));
```

### SQLite Schema (migrations/sqlite/001_initial.sql)

Similar to PostgreSQL but with SQLite-specific syntax:
- Use `INTEGER PRIMARY KEY AUTOINCREMENT` where needed
- Remove `JSONB`, use `TEXT` for JSON columns
- Adjust timestamp handling (SQLite uses TEXT, INTEGER, or REAL for dates)

## Archiver Implementation (archiver.go)

```go
package storage

import (
    "context"

    graw "github.com/jamesprial/go-reddit-api-wrapper"
    "github.com/jamesprial/go-reddit-api-wrapper/pkg/types"
)

// Archiver combines Reddit API client with storage backend
type Archiver struct {
    client  *graw.Client
    storage Storage
}

// NewArchiver creates a new archiver instance
func NewArchiver(client *graw.Client, storage Storage) *Archiver {
    return &Archiver{
        client:  client,
        storage: storage,
    }
}

// ArchiveOptions configures archiving behavior
type ArchiveOptions struct {
    Sort            string // "hot", "new", "top"
    Limit           int    // Max posts to fetch per batch
    IncludeComments bool   // Whether to archive comments
    MaxCommentDepth int    // Maximum depth for comment trees
    UpdateExisting  bool   // Re-fetch and update existing posts
}

// ArchiveSubreddit fetches and stores posts from a subreddit
func (a *Archiver) ArchiveSubreddit(ctx context.Context, subreddit string, opts ArchiveOptions) error

// ArchivePost fetches and stores a single post with comments
func (a *Archiver) ArchivePost(ctx context.Context, subreddit, postID string, includeComments bool) error

// ContinuousArchive continuously monitors and archives new content
func (a *Archiver) ContinuousArchive(ctx context.Context, subreddit string, interval time.Duration) error

// UpdateScores refreshes scores for recently archived posts
func (a *Archiver) UpdateScores(ctx context.Context, subreddit string, maxAge time.Duration) error
```

## Implementation Details

### PostgreSQL Implementation (postgres/postgres.go)

```go
package postgres

import (
    "context"
    "database/sql"
    "encoding/json"

    _ "github.com/lib/pq"
    "github.com/jamesprial/go-reddit-api-wrapper/pkg/types"
    "github.com/jamesprial/go-reddit-storage"
)

type PostgresStorage struct {
    db *sql.DB
}

func New(connString string) (*PostgresStorage, error) {
    db, err := sql.Open("postgres", connString)
    if err != nil {
        return nil, err
    }

    if err := db.Ping(); err != nil {
        return nil, err
    }

    return &PostgresStorage{db: db}, nil
}

func (s *PostgresStorage) SavePost(ctx context.Context, post *types.Post) error {
    // Use INSERT ... ON CONFLICT UPDATE for idempotency
    query := `
        INSERT INTO posts (
            id, subreddit, author, title, selftext, url,
            score, upvote_ratio, num_comments, created_utc,
            edited_utc, is_self, is_video, raw_json, last_updated
        ) VALUES (
            $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NOW()
        )
        ON CONFLICT (id) DO UPDATE SET
            score = EXCLUDED.score,
            num_comments = EXCLUDED.num_comments,
            last_updated = NOW()
    `

    rawJSON, _ := json.Marshal(post)

    _, err := s.db.ExecContext(ctx, query,
        post.ID, post.Subreddit, post.Author, post.Title,
        post.Selftext, post.URL, post.Score, post.UpvoteRatio,
        post.NumComments, post.CreatedUTC, post.EditedUTC,
        post.IsSelf, post.IsVideo, rawJSON,
    )

    return err
}

// Implement remaining Storage interface methods...
```

### Key Implementation Notes

1. **Idempotency:** Use `INSERT ... ON CONFLICT` (Postgres) or `INSERT OR REPLACE` (SQLite) to handle duplicate entries

2. **Batch Operations:** Use transactions and prepared statements for bulk inserts:
```go
func (s *PostgresStorage) SavePosts(ctx context.Context, posts []*types.Post) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    stmt, err := tx.PrepareContext(ctx, insertQuery)
    if err != nil {
        return err
    }
    defer stmt.Close()

    for _, post := range posts {
        _, err = stmt.ExecContext(ctx, /* post fields */)
        if err != nil {
            return err
        }
    }

    return tx.Commit()
}
```

3. **Comment Threading:** Store `depth` field and use recursive CTEs for queries:
```sql
WITH RECURSIVE comment_tree AS (
    SELECT *, 0 as level FROM comments WHERE post_id = $1 AND parent_id IS NULL
    UNION ALL
    SELECT c.*, ct.level + 1 FROM comments c
    JOIN comment_tree ct ON c.parent_id = ct.id
)
SELECT * FROM comment_tree ORDER BY level, created_utc;
```

4. **Error Handling:** Define custom error types:
```go
type StorageError struct {
    Op  string // Operation being performed
    Err error  // Underlying error
}
```

## Testing Strategy

### Test Structure

```go
// postgres/postgres_test.go
func TestPostgresStorage_SavePost(t *testing.T) {
    // Use testcontainers or similar for real Postgres instance
    // Or mock sql.DB for unit tests
}

func TestPostgresStorage_SavePosts_Batch(t *testing.T) {
    // Test bulk operations
}

func TestPostgresStorage_Idempotency(t *testing.T) {
    // Verify saving same post twice doesn't error
}
```

### Integration Tests

Use Docker/testcontainers for real database tests:
```bash
# Start test databases
docker run -d --name test-postgres -e POSTGRES_PASSWORD=test postgres:15
docker run -d --name test-sqlite ...

# Run tests
go test -v ./...
```

## CLI Tool (cmd/reddit-archiver/main.go)

```go
package main

import (
    "context"
    "flag"
    "log"

    graw "github.com/jamesprial/go-reddit-api-wrapper"
    "github.com/jamesprial/go-reddit-storage"
    "github.com/jamesprial/go-reddit-storage/postgres"
)

func main() {
    var (
        subreddit = flag.String("subreddit", "", "Subreddit to archive")
        dbURL     = flag.String("db", "", "Database connection string")
        sort      = flag.String("sort", "hot", "Sort: hot, new, top")
        limit     = flag.Int("limit", 25, "Number of posts")
        comments  = flag.Bool("comments", true, "Include comments")
    )
    flag.Parse()

    // Initialize storage
    store, err := postgres.New(*dbURL)
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()

    // Initialize Reddit client
    client, err := graw.NewClient(&graw.Config{
        ClientID:     os.Getenv("REDDIT_CLIENT_ID"),
        ClientSecret: os.Getenv("REDDIT_CLIENT_SECRET"),
        UserAgent:    "reddit-archiver/1.0",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Archive
    archiver := storage.NewArchiver(client, store)
    opts := storage.ArchiveOptions{
        Sort:            *sort,
        Limit:           *limit,
        IncludeComments: *comments,
    }

    if err := archiver.ArchiveSubreddit(context.Background(), *subreddit, opts); err != nil {
        log.Fatal(err)
    }

    log.Printf("Successfully archived r/%s", *subreddit)
}
```

## Example Usage

### Basic Usage (examples/basic/main.go)

```go
package main

import (
    "context"
    "log"

    graw "github.com/jamesprial/go-reddit-api-wrapper"
    "github.com/jamesprial/go-reddit-storage"
    "github.com/jamesprial/go-reddit-storage/postgres"
)

func main() {
    // Setup storage
    store, _ := postgres.New("postgres://user:pass@localhost/reddit")
    defer store.Close()

    // Run migrations
    if err := store.RunMigrations(context.Background()); err != nil {
        log.Fatal(err)
    }

    // Setup Reddit client
    client, _ := graw.NewClient(&graw.Config{
        ClientID:     "your-id",
        ClientSecret: "your-secret",
        UserAgent:    "my-archiver/1.0",
    })

    // Create archiver
    archiver := storage.NewArchiver(client, store)

    // Archive subreddit
    opts := storage.ArchiveOptions{
        Sort:            "hot",
        Limit:           100,
        IncludeComments: true,
        MaxCommentDepth: 10,
    }

    if err := archiver.ArchiveSubreddit(context.Background(), "golang", opts); err != nil {
        log.Fatal(err)
    }

    // Query stored data
    posts, _ := store.GetPostsBySubreddit(context.Background(), "golang", storage.QueryOptions{
        Limit:  10,
        SortBy: "score",
    })

    for _, post := range posts {
        log.Printf("Archived: %s (score: %d)", post.Title, post.Score)
    }
}
```

### Continuous Monitoring (examples/continuous/main.go)

```go
func main() {
    // ... setup ...

    // Monitor subreddit every 5 minutes
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            opts := storage.ArchiveOptions{
                Sort:            "new",
                Limit:           25,
                IncludeComments: true,
            }

            if err := archiver.ArchiveSubreddit(ctx, "golang", opts); err != nil {
                log.Printf("Error archiving: %v", err)
            }

        case <-ctx.Done():
            return
        }
    }
}
```

## Key Commands

```bash
# Install dependencies
go mod init github.com/<username>/go-reddit-storage
go get github.com/jamesprial/go-reddit-api-wrapper@v0.1.0
go get github.com/lib/pq
go get modernc.org/sqlite

# Run tests
go test -v ./...

# Run tests with database
docker-compose up -d  # Start test databases
go test -v -tags=integration ./...

# Build CLI
go build -o reddit-archiver ./cmd/reddit-archiver

# Run CLI
export REDDIT_CLIENT_ID="..."
export REDDIT_CLIENT_SECRET="..."
./reddit-archiver -subreddit golang -db "postgres://localhost/reddit"

# Run example
go run examples/basic/main.go
```

## Dependencies

```go
// go.mod
module github.com/<username>/go-reddit-storage

go 1.25

require (
    github.com/jamesprial/go-reddit-api-wrapper v0.1.0
    github.com/lib/pq v1.10.9                    // PostgreSQL driver
    modernc.org/sqlite v1.27.0                   // SQLite driver (pure Go)
)
```

## README Structure

```markdown
# Go Reddit Storage

Database persistence layer for Reddit data fetched via go-reddit-api-wrapper.

## Features
- PostgreSQL and SQLite backends
- Idempotent storage operations
- Comment threading preservation
- Bulk operations for performance
- High-level archiving utilities
- CLI tool for quick archiving

## Installation
[...]

## Quick Start
[...]

## Database Schema
[...]

## Examples
[...]

## CLI Usage
[...]
```

## Implementation Order

1. **Phase 1 - Core Storage**
   - Define interfaces in `storage.go`
   - Implement PostgreSQL backend
   - Write basic tests
   - Create initial migrations

2. **Phase 2 - Archiver**
   - Implement `Archiver` in `archiver.go`
   - Add batch archiving logic
   - Test with live API

3. **Phase 3 - SQLite**
   - Implement SQLite backend
   - Share tests between backends

4. **Phase 4 - CLI & Examples**
   - Build CLI tool
   - Create example programs
   - Write documentation

5. **Phase 5 - Advanced Features**
   - Add search functionality
   - Implement continuous monitoring
   - Add statistics/analytics queries

## Notes for Implementation

- Use `database/sql` for database abstraction
- Consider using `sqlx` for easier query building (optional)
- Store raw JSON in JSONB/TEXT fields for future schema evolution
- Use prepared statements for security and performance
- Implement proper connection pooling
- Add structured logging (slog)
- Consider rate limiting coordination with Reddit API wrapper
- Handle timezone conversions properly (store UTC, display local)
- Add metrics/observability hooks for production use

## Success Criteria

- [ ] Can archive posts and comments from any subreddit
- [ ] Handles re-archiving same content without errors
- [ ] Efficient bulk operations (>100 posts/sec)
- [ ] Comment trees preserved correctly
- [ ] Both PostgreSQL and SQLite work identically (interface-wise)
- [ ] CLI tool works for basic archiving tasks
- [ ] Comprehensive test coverage (>80%)
- [ ] Clear documentation and examples
- [ ] Tagged v0.1.0 release