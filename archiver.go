package storage

import (
	"context"
	"fmt"
	"log"
	"time"

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
func (a *Archiver) ArchiveSubreddit(ctx context.Context, subreddit string, opts ArchiveOptions) error {
	// Fetch subreddit info first
	subInfo, err := a.client.GetSubreddit(ctx, subreddit)
	if err != nil {
		return &StorageError{Op: "fetch_subreddit", Err: err}
	}

	if err := a.storage.SaveSubreddit(ctx, subInfo); err != nil {
		return err
	}

	// Set defaults
	if opts.Limit == 0 {
		opts.Limit = 25
	}
	if opts.Sort == "" {
		opts.Sort = "hot"
	}

	// Fetch posts based on sort type
	var postsResponse *types.PostsResponse
	req := &types.PostsRequest{
		Subreddit: subreddit,
		Pagination: types.Pagination{
			Limit: opts.Limit,
		},
	}

	switch opts.Sort {
	case "hot":
		postsResponse, err = a.client.GetHot(ctx, req)
	case "new", "top":
		// Note: "top" is not yet supported by the API wrapper, so we use "new"
		postsResponse, err = a.client.GetNew(ctx, req)
	default:
		return &StorageError{Op: "archive_subreddit", Err: fmt.Errorf("invalid sort type: %s", opts.Sort)}
	}

	if err != nil {
		return &StorageError{Op: "fetch_posts", Err: err}
	}

	posts := postsResponse.Posts

	// Save posts
	if err := a.storage.SavePosts(ctx, posts); err != nil {
		return err
	}

	// Archive comments if requested
	if opts.IncludeComments {
		for _, post := range posts {
			if err := a.ArchivePost(ctx, subreddit, post.ID, true); err != nil {
				// Log error but continue with other posts
				log.Printf("Error archiving comments for post %s: %v", post.ID, err)
			}
		}
	}

	return nil
}

// ArchivePost fetches and stores a single post with comments
func (a *Archiver) ArchivePost(ctx context.Context, subreddit, postID string, includeComments bool) error {
	// Fetch post and comments
	commentsReq := &types.CommentsRequest{
		Subreddit: subreddit,
		PostID:    postID,
	}

	commentsResp, err := a.client.GetComments(ctx, commentsReq)
	if err != nil {
		return &StorageError{Op: "fetch_post_and_comments", Err: err}
	}

	// Save post
	if err := a.storage.SavePost(ctx, commentsResp.Post); err != nil {
		return err
	}

	// Save comments if requested and available
	if includeComments && len(commentsResp.Comments) > 0 {
		if err := a.storage.SaveComments(ctx, commentsResp.Comments); err != nil {
			return err
		}
	}

	return nil
}

// ContinuousArchive continuously monitors and archives new content
func (a *Archiver) ContinuousArchive(ctx context.Context, subreddit string, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial archive
	opts := ArchiveOptions{
		Sort:            "new",
		Limit:           25,
		IncludeComments: true,
	}

	if err := a.ArchiveSubreddit(ctx, subreddit, opts); err != nil {
		log.Printf("Error during initial archive: %v", err)
	}

	// Continuous monitoring
	for {
		select {
		case <-ticker.C:
			if err := a.ArchiveSubreddit(ctx, subreddit, opts); err != nil {
				log.Printf("Error during continuous archive: %v", err)
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// UpdateScores refreshes scores for recently archived posts
func (a *Archiver) UpdateScores(ctx context.Context, subreddit string, maxAge time.Duration) error {
	// Calculate cutoff time
	cutoff := time.Now().Add(-maxAge)

	// Fetch recent posts from storage
	opts := QueryOptions{
		Limit:     100,
		SortBy:    "created",
		SortOrder: "desc",
		StartDate: cutoff,
	}

	posts, err := a.storage.GetPostsBySubreddit(ctx, subreddit, opts)
	if err != nil {
		return err
	}

	// Update each post
	for _, post := range posts {
		commentsReq := &types.CommentsRequest{
			Subreddit: subreddit,
			PostID:    post.ID,
		}

		commentsResp, err := a.client.GetComments(ctx, commentsReq)
		if err != nil {
			log.Printf("Error fetching updated post %s: %v", post.ID, err)
			continue
		}

		if err := a.storage.SavePost(ctx, commentsResp.Post); err != nil {
			log.Printf("Error saving updated post %s: %v", post.ID, err)
			continue
		}
	}

	return nil
}

// BackfillSubreddit archives historical posts from a subreddit
func (a *Archiver) BackfillSubreddit(ctx context.Context, subreddit string, maxPosts int, includeComments bool) error {
	fetched := 0
	after := ""

	for fetched < maxPosts {
		// Calculate batch size
		batchSize := 100
		if maxPosts-fetched < batchSize {
			batchSize = maxPosts - fetched
		}

		// Fetch batch of posts
		req := &types.PostsRequest{
			Subreddit: subreddit,
			Pagination: types.Pagination{
				Limit: batchSize,
				After: after,
			},
		}

		postsResponse, err := a.client.GetNew(ctx, req)
		if err != nil {
			return &StorageError{Op: "backfill_fetch", Err: err}
		}

		if len(postsResponse.Posts) == 0 {
			break // No more posts
		}

		// Save posts
		if err := a.storage.SavePosts(ctx, postsResponse.Posts); err != nil {
			return err
		}

		// Archive comments if requested
		if includeComments {
			for _, post := range postsResponse.Posts {
				if err := a.ArchivePost(ctx, subreddit, post.ID, true); err != nil {
					log.Printf("Error archiving comments for post %s: %v", post.ID, err)
				}
			}
		}

		fetched += len(postsResponse.Posts)
		log.Printf("Backfilled %d/%d posts from r/%s", fetched, maxPosts, subreddit)

		// Update after parameter for pagination
		after = postsResponse.AfterFullname
		if after == "" {
			break // No more pages
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	return nil
}