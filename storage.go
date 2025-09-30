package storage

import (
	"context"
	"fmt"
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
	SaveSubreddit(ctx context.Context, sub *types.SubredditData) error
	GetSubreddit(ctx context.Context, name string) (*types.SubredditData, error)

	// Queries
	SearchPosts(ctx context.Context, query string, opts QueryOptions) ([]*types.Post, error)
	GetPostStats(ctx context.Context, postID string) (*PostStats, error)

	// Management
	RunMigrations(ctx context.Context) error
	Close() error
}

// QueryOptions provides filtering and pagination for queries
type QueryOptions struct {
	Limit     int
	Offset    int
	SortBy    string    // "created", "score", "comments"
	SortOrder string    // "asc", "desc"
	StartDate time.Time
	EndDate   time.Time
}

// PostStats aggregates statistics about a post
type PostStats struct {
	PostID          string
	CommentCount    int
	MaxCommentDepth int
	LastUpdated     time.Time
}

// StorageError represents a storage operation error
type StorageError struct {
	Op  string // Operation being performed
	Err error  // Underlying error
}

func (e *StorageError) Error() string {
	return fmt.Sprintf("storage error during %s: %v", e.Op, e.Err)
}

func (e *StorageError) Unwrap() error {
	return e.Err
}