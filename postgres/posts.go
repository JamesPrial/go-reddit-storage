package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jamesprial/go-reddit-api-wrapper/pkg/types"
	"github.com/jamesprial/go-reddit-storage"
)

// SavePost saves or updates a single post
func (s *PostgresStorage) SavePost(ctx context.Context, post *types.Post) error {
	// Ensure subreddit exists first
	if post.Subreddit != "" {
		sub := &types.SubredditData{DisplayName: post.Subreddit}
		if err := s.SaveSubreddit(ctx, sub); err != nil {
			return err
		}
	}

	rawJSON, err := json.Marshal(post)
	if err != nil {
		return &storage.StorageError{Op: "marshal_post", Err: err}
	}

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
			edited_utc = EXCLUDED.edited_utc,
			last_updated = NOW(),
			raw_json = EXCLUDED.raw_json
	`

	// Handle edited timestamp
	var editedUTC interface{}
	if post.Edited.IsEdited && post.Edited.Timestamp > 0 {
		editedUTC = post.Edited.Timestamp
	}

	_, err = s.db.ExecContext(ctx, query,
		post.ID, post.Subreddit, post.Author, post.Title,
		post.SelfText, post.URL, post.Score, nil, // upvote_ratio not available
		post.NumComments, post.CreatedUTC, editedUTC,
		post.IsSelf, false, rawJSON, // is_video not available
	)

	if err != nil {
		return &storage.StorageError{Op: "save_post", Err: err}
	}

	return nil
}

// SavePosts saves or updates multiple posts in a transaction
func (s *PostgresStorage) SavePosts(ctx context.Context, posts []*types.Post) error {
	if len(posts) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return &storage.StorageError{Op: "begin_transaction", Err: err}
	}
	defer tx.Rollback()

	// Prepare statement for posts
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
			upvote_ratio = EXCLUDED.upvote_ratio,
			edited_utc = EXCLUDED.edited_utc,
			last_updated = NOW(),
			raw_json = EXCLUDED.raw_json
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return &storage.StorageError{Op: "prepare_statement", Err: err}
	}
	defer stmt.Close()

	// Ensure subreddits exist
	subreddits := make(map[string]bool)
	for _, post := range posts {
		if post.Subreddit != "" && !subreddits[post.Subreddit] {
			sub := &types.SubredditData{DisplayName: post.Subreddit}
			if err := s.SaveSubreddit(ctx, sub); err != nil {
				return err
			}
			subreddits[post.Subreddit] = true
		}
	}

	// Insert posts
	for _, post := range posts {
		rawJSON, err := json.Marshal(post)
		if err != nil {
			return &storage.StorageError{Op: "marshal_post", Err: err}
		}

		// Handle edited timestamp
		var editedUTC interface{}
		if post.Edited.IsEdited && post.Edited.Timestamp > 0 {
			editedUTC = post.Edited.Timestamp
		}

		_, err = stmt.ExecContext(ctx,
			post.ID, post.Subreddit, post.Author, post.Title,
			post.SelfText, post.URL, post.Score, nil, // upvote_ratio not available
			post.NumComments, post.CreatedUTC, editedUTC,
			post.IsSelf, false, rawJSON, // is_video not available
		)

		if err != nil {
			return &storage.StorageError{Op: "insert_post", Err: err}
		}
	}

	if err := tx.Commit(); err != nil {
		return &storage.StorageError{Op: "commit_transaction", Err: err}
	}

	return nil
}

// GetPost retrieves a single post by ID
func (s *PostgresStorage) GetPost(ctx context.Context, id string) (*types.Post, error) {
	query := `
		SELECT id, subreddit, author, title, selftext, url, score, upvote_ratio,
		       num_comments, created_utc, edited_utc, is_self, is_video, raw_json
		FROM posts
		WHERE id = $1
	`

	var post types.Post
	var rawJSON []byte

	var upvoteRatio sql.NullFloat64
	var isVideo bool
	var editedUTC sql.NullFloat64

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&post.ID, &post.Subreddit, &post.Author, &post.Title,
		&post.SelfText, &post.URL, &post.Score, &upvoteRatio,
		&post.NumComments, &post.CreatedUTC, &editedUTC,
		&post.IsSelf, &isVideo, &rawJSON,
	)

	// Reconstruct Edited field
	if editedUTC.Valid {
		post.Edited = types.Edited{IsEdited: true, Timestamp: editedUTC.Float64}
	} else {
		post.Edited = types.Edited{IsEdited: false}
	}

	if err == sql.ErrNoRows {
		return nil, &storage.StorageError{Op: "get_post", Err: fmt.Errorf("post not found: %s", id)}
	}

	if err != nil {
		return nil, &storage.StorageError{Op: "get_post", Err: err}
	}

	return &post, nil
}

// GetPostsBySubreddit retrieves posts from a subreddit with filtering options
func (s *PostgresStorage) GetPostsBySubreddit(ctx context.Context, subreddit string, opts storage.QueryOptions) ([]*types.Post, error) {
	// Build query with options
	query := `
		SELECT id, subreddit, author, title, selftext, url, score, upvote_ratio,
		       num_comments, created_utc, edited_utc, is_self, is_video, raw_json
		FROM posts
		WHERE subreddit = $1
	`

	var args []interface{}
	args = append(args, subreddit)
	argPos := 2

	// Add date filters if provided
	if !opts.StartDate.IsZero() {
		query += fmt.Sprintf(" AND created_utc >= $%d", argPos)
		args = append(args, opts.StartDate)
		argPos++
	}

	if !opts.EndDate.IsZero() {
		query += fmt.Sprintf(" AND created_utc <= $%d", argPos)
		args = append(args, opts.EndDate)
		argPos++
	}

	// Add sorting
	sortBy := opts.SortBy
	if sortBy == "" {
		sortBy = "created_utc"
	}

	sortOrder := strings.ToUpper(opts.SortOrder)
	if sortOrder != "ASC" && sortOrder != "DESC" {
		sortOrder = "DESC"
	}

	// Validate sort column to prevent SQL injection
	validSortColumns := map[string]bool{
		"created_utc":  true,
		"created":      true,
		"score":        true,
		"num_comments": true,
		"comments":     true,
	}

	if sortBy == "comments" {
		sortBy = "num_comments"
	} else if sortBy == "created" {
		sortBy = "created_utc"
	}

	if !validSortColumns[sortBy] {
		sortBy = "created_utc"
	}

	query += fmt.Sprintf(" ORDER BY %s %s", sortBy, sortOrder)

	// Add pagination
	limit := opts.Limit
	if limit == 0 {
		limit = 25
	}

	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argPos, argPos+1)
	args = append(args, limit, opts.Offset)

	// Execute query
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, &storage.StorageError{Op: "get_posts_by_subreddit", Err: err}
	}
	defer rows.Close()

	return s.scanPosts(rows)
}