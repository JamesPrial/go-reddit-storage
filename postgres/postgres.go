package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"

	"github.com/jamesprial/go-reddit-api-wrapper/pkg/types"
	"github.com/jamesprial/go-reddit-storage"
	"github.com/jamesprial/go-reddit-storage/schema"
)

// PostgresStorage implements the Storage interface for PostgreSQL
type PostgresStorage struct {
	db *sql.DB
}

// PoolConfig configures the PostgreSQL connection pool
type PoolConfig struct {
	// MaxOpenConns sets the maximum number of open connections to the database
	// Default: 0 (unlimited)
	MaxOpenConns int

	// MaxIdleConns sets the maximum number of connections in the idle connection pool
	// Default: 2
	MaxIdleConns int

	// ConnMaxLifetime sets the maximum amount of time a connection may be reused
	// Default: 0 (connections are reused forever)
	ConnMaxLifetime time.Duration

	// ConnMaxIdleTime sets the maximum amount of time a connection may be idle
	// Default: 0 (connections are not closed due to idle time)
	ConnMaxIdleTime time.Duration
}

// DefaultPoolConfig returns sensible defaults for production use
func DefaultPoolConfig() *PoolConfig {
	return &PoolConfig{
		MaxOpenConns:    25,               // Reasonable limit for most applications
		MaxIdleConns:    5,                // Keep some connections ready
		ConnMaxLifetime: 5 * time.Minute,  // Rotate connections periodically
		ConnMaxIdleTime: 10 * time.Minute, // Close idle connections after 10 minutes
	}
}

// New creates a new PostgreSQL storage instance with default pool configuration
func New(connString string) (*PostgresStorage, error) {
	return NewWithPool(connString, DefaultPoolConfig())
}

// NewWithPool creates a new PostgreSQL storage instance with custom pool configuration
func NewWithPool(connString string, config *PoolConfig) (*PostgresStorage, error) {
	db, err := sql.Open("postgres", connString)
	if err != nil {
		return nil, &storage.StorageError{Op: "open", Err: err}
	}

	// Apply pool configuration
	if config != nil {
		db.SetMaxOpenConns(config.MaxOpenConns)
		db.SetMaxIdleConns(config.MaxIdleConns)
		db.SetConnMaxLifetime(config.ConnMaxLifetime)
		db.SetConnMaxIdleTime(config.ConnMaxIdleTime)
	}

	if err := db.Ping(); err != nil {
		return nil, &storage.StorageError{Op: "ping", Err: err}
	}

	return &PostgresStorage{db: db}, nil
}

// RunMigrations runs all pending database migrations
func (s *PostgresStorage) RunMigrations(ctx context.Context) error {
	runner, err := schema.NewMigrationRunner(s.db, "postgres")
	if err != nil {
		return &storage.StorageError{Op: "create_migration_runner", Err: err}
	}

	if err := runner.Run(ctx); err != nil {
		return &storage.StorageError{Op: "run_migrations", Err: err}
	}

	return nil
}

// Close closes the database connection
func (s *PostgresStorage) Close() error {
	if err := s.db.Close(); err != nil {
		return &storage.StorageError{Op: "close", Err: err}
	}
	return nil
}

// SaveSubreddit saves or updates a subreddit
func (s *PostgresStorage) SaveSubreddit(ctx context.Context, sub *types.SubredditData) error {
	rawJSON, err := json.Marshal(sub)
	if err != nil {
		return &storage.StorageError{Op: "marshal_subreddit", Err: err}
	}

	query := `
		INSERT INTO subreddits (
			name, display_name, title, description, subscribers,
			created_utc, raw_json, last_synced
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (name) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			title = EXCLUDED.title,
			description = EXCLUDED.description,
			subscribers = EXCLUDED.subscribers,
			last_synced = NOW(),
			raw_json = EXCLUDED.raw_json
	`

	_, err = s.db.ExecContext(ctx, query,
		sub.DisplayName, sub.DisplayName, sub.Title, sub.Description,
		sub.Subscribers, nil, rawJSON, // created_utc not available in API
	)

	if err != nil {
		return &storage.StorageError{Op: "save_subreddit", Err: err}
	}

	return nil
}

// GetSubreddit retrieves a subreddit by name
func (s *PostgresStorage) GetSubreddit(ctx context.Context, name string) (*types.SubredditData, error) {
	query := `
		SELECT name, display_name, title, description, subscribers, created_utc, raw_json
		FROM subreddits
		WHERE name = $1
	`

	var sub types.SubredditData
	var rawJSON []byte
	var createdUTC sql.NullTime

	err := s.db.QueryRowContext(ctx, query, name).Scan(
		&sub.DisplayName, &sub.DisplayName, &sub.Title, &sub.Description,
		&sub.Subscribers, &createdUTC, &rawJSON,
	)

	if err == sql.ErrNoRows {
		return nil, &storage.StorageError{Op: "get_subreddit", Err: fmt.Errorf("subreddit not found: %s", name)}
	}

	if err != nil {
		return nil, &storage.StorageError{Op: "get_subreddit", Err: err}
	}

	return &sub, nil
}

// SearchPosts searches for posts using full-text search
func (s *PostgresStorage) SearchPosts(ctx context.Context, query string, opts storage.QueryOptions) ([]*types.Post, error) {
	sqlQuery := `
		SELECT id, subreddit, author, title, selftext, url, score, upvote_ratio,
		       num_comments, created_utc, edited_utc, is_self, is_video, raw_json
		FROM posts
		WHERE to_tsvector('english', title || ' ' || COALESCE(selftext, '')) @@ plainto_tsquery('english', $1)
		ORDER BY score DESC
		LIMIT $2 OFFSET $3
	`

	limit := opts.Limit
	if limit == 0 {
		limit = 25
	}

	rows, err := s.db.QueryContext(ctx, sqlQuery, query, limit, opts.Offset)
	if err != nil {
		return nil, &storage.StorageError{Op: "search_posts", Err: err}
	}
	defer rows.Close()

	return s.scanPosts(rows)
}

// GetPostStats returns statistics about a post
func (s *PostgresStorage) GetPostStats(ctx context.Context, postID string) (*storage.PostStats, error) {
	query := `
		WITH RECURSIVE comment_tree AS (
			SELECT id, depth, 0 as level
			FROM comments
			WHERE post_id = $1 AND parent_id IS NULL
			UNION ALL
			SELECT c.id, c.depth, ct.level + 1
			FROM comments c
			JOIN comment_tree ct ON c.parent_id = ct.id
		)
		SELECT
			COUNT(ct.id) as comment_count,
			COALESCE(MAX(level), 0) as max_depth,
			MAX(p.last_updated) as last_updated
		FROM posts p
		LEFT JOIN comment_tree ct ON 1=1
		WHERE p.id = $1
		GROUP BY p.id
	`

	var stats storage.PostStats
	stats.PostID = postID

	err := s.db.QueryRowContext(ctx, query, postID).Scan(
		&stats.CommentCount, &stats.MaxCommentDepth, &stats.LastUpdated,
	)

	if err != nil {
		return nil, &storage.StorageError{Op: "get_post_stats", Err: err}
	}

	return &stats, nil
}

// scanPosts is a helper function to scan post rows
func (s *PostgresStorage) scanPosts(rows *sql.Rows) ([]*types.Post, error) {
	var posts []*types.Post

	for rows.Next() {
		var post types.Post
		var rawJSON []byte
		var upvoteRatio sql.NullFloat64
		var isVideo bool
		var createdAt time.Time
		var editedUTC sql.NullTime

		err := rows.Scan(
			&post.ID, &post.Subreddit, &post.Author, &post.Title,
			&post.SelfText, &post.URL, &post.Score, &upvoteRatio,
			&post.NumComments, &createdAt, &editedUTC,
			&post.IsSelf, &isVideo, &rawJSON,
		)

		if err != nil {
			return nil, &storage.StorageError{Op: "scan_post", Err: err}
		}

		post.CreatedUTC = timeToUnixFloat(createdAt)

		// Reconstruct Edited field
		if editedUTC.Valid {
			post.Edited = types.Edited{IsEdited: true, Timestamp: timeToUnixFloat(editedUTC.Time)}
		} else {
			post.Edited = types.Edited{IsEdited: false}
		}

		posts = append(posts, &post)
	}

	if err := rows.Err(); err != nil {
		return nil, &storage.StorageError{Op: "scan_posts", Err: err}
	}

	return posts, nil
}
