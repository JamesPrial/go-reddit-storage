package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/jamesprial/go-reddit-api-wrapper/pkg/types"
	"github.com/jamesprial/go-reddit-storage"
	"github.com/jamesprial/go-reddit-storage/schema"
)

// SQLiteStorage implements the Storage interface for SQLite
type SQLiteStorage struct {
	db *sql.DB
}

// New creates a new SQLite storage instance
func New(dbPath string) (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, &storage.StorageError{Op: "open", Err: err}
	}

	// Enable foreign keys and WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, &storage.StorageError{Op: "enable_foreign_keys", Err: err}
	}

	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return nil, &storage.StorageError{Op: "enable_wal", Err: err}
	}

	return &SQLiteStorage{db: db}, nil
}

// RunMigrations runs all pending database migrations
func (s *SQLiteStorage) RunMigrations(ctx context.Context) error {
	runner, err := schema.NewMigrationRunner(s.db, "sqlite")
	if err != nil {
		return &storage.StorageError{Op: "create_migration_runner", Err: err}
	}

	if err := runner.Run(ctx); err != nil {
		return &storage.StorageError{Op: "run_migrations", Err: err}
	}

	return nil
}

// Close closes the database connection
func (s *SQLiteStorage) Close() error {
	if err := s.db.Close(); err != nil {
		return &storage.StorageError{Op: "close", Err: err}
	}
	return nil
}

// SaveSubreddit saves or updates a subreddit
func (s *SQLiteStorage) SaveSubreddit(ctx context.Context, sub *types.SubredditData) error {
	rawJSON, err := json.Marshal(sub)
	if err != nil {
		return &storage.StorageError{Op: "marshal_subreddit", Err: err}
	}

	query := `
		INSERT INTO subreddits (
			name, display_name, title, description, subscribers,
			created_utc, raw_json, last_synced
		) VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT (name) DO UPDATE SET
			display_name = excluded.display_name,
			title = excluded.title,
			description = excluded.description,
			subscribers = excluded.subscribers,
			last_synced = CURRENT_TIMESTAMP,
			raw_json = excluded.raw_json
	`

	_, err = s.db.ExecContext(ctx, query,
		sub.DisplayName, sub.DisplayName, sub.Title, sub.Description,
		sub.Subscribers, nil, string(rawJSON), // created_utc not available
	)

	if err != nil {
		return &storage.StorageError{Op: "save_subreddit", Err: err}
	}

	return nil
}

// GetSubreddit retrieves a subreddit by name
func (s *SQLiteStorage) GetSubreddit(ctx context.Context, name string) (*types.SubredditData, error) {
	query := `
		SELECT name, display_name, title, description, subscribers, created_utc, raw_json
		FROM subreddits
		WHERE name = ?
	`

	var sub types.SubredditData
	var rawJSON string
	var createdUTC sql.NullString

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

// SearchPosts searches for posts (basic implementation for SQLite)
func (s *SQLiteStorage) SearchPosts(ctx context.Context, query string, opts storage.QueryOptions) ([]*types.Post, error) {
	// SQLite doesn't have full-text search by default, so we use LIKE
	sqlQuery := `
		SELECT id, subreddit, author, title, selftext, url, score, upvote_ratio,
		       num_comments, created_utc, edited_utc, is_self, is_video, raw_json
		FROM posts
		WHERE title LIKE ? OR selftext LIKE ?
		ORDER BY score DESC
		LIMIT ? OFFSET ?
	`

	limit := opts.Limit
	if limit == 0 {
		limit = 25
	}

	searchPattern := "%" + query + "%"
	rows, err := s.db.QueryContext(ctx, sqlQuery, searchPattern, searchPattern, limit, opts.Offset)
	if err != nil {
		return nil, &storage.StorageError{Op: "search_posts", Err: err}
	}
	defer rows.Close()

	return s.scanPosts(rows)
}

// GetPostStats returns statistics about a post
func (s *SQLiteStorage) GetPostStats(ctx context.Context, postID string) (*storage.PostStats, error) {
	query := `
		WITH RECURSIVE comment_tree AS (
			SELECT id, depth, 0 as level
			FROM comments
			WHERE post_id = ? AND parent_id IS NULL
			UNION ALL
			SELECT c.id, c.depth, ct.level + 1
			FROM comments c
			JOIN comment_tree ct ON c.parent_id = ct.id
		)
		SELECT
			COUNT(*) as comment_count,
			COALESCE(MAX(level), 0) as max_depth,
			MAX(p.last_updated) as last_updated
		FROM posts p
		LEFT JOIN comment_tree ct ON 1=1
		WHERE p.id = ?
		GROUP BY p.id
	`

	var stats storage.PostStats
	stats.PostID = postID

	err := s.db.QueryRowContext(ctx, query, postID, postID).Scan(
		&stats.CommentCount, &stats.MaxCommentDepth, &stats.LastUpdated,
	)

	if err != nil {
		return nil, &storage.StorageError{Op: "get_post_stats", Err: err}
	}

	return &stats, nil
}

// scanPosts is a helper function to scan post rows
func (s *SQLiteStorage) scanPosts(rows *sql.Rows) ([]*types.Post, error) {
	var posts []*types.Post

	for rows.Next() {
		var post types.Post
		var rawJSON string
		var isSelf, isVideo int
		var upvoteRatio sql.NullFloat64
		var editedUTC sql.NullString

		err := rows.Scan(
			&post.ID, &post.Subreddit, &post.Author, &post.Title,
			&post.SelfText, &post.URL, &post.Score, &upvoteRatio,
			&post.NumComments, &post.CreatedUTC, &editedUTC,
			&isSelf, &isVideo, &rawJSON,
		)

		if err != nil {
			return nil, &storage.StorageError{Op: "scan_post", Err: err}
		}

		post.IsSelf = isSelf != 0

		// Reconstruct Edited field
		if editedUTC.Valid {
			var timestamp float64
			if _, err := fmt.Sscanf(editedUTC.String, "%f", &timestamp); err == nil {
				post.Edited = types.Edited{IsEdited: true, Timestamp: timestamp}
			} else {
				post.Edited = types.Edited{IsEdited: false}
			}
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