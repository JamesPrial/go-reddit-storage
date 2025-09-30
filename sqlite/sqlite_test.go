package sqlite

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jamesprial/go-reddit-api-wrapper/pkg/types"
	"github.com/jamesprial/go-reddit-storage"
)

// getTestDB returns a test database connection
func getTestDB(t *testing.T) *SQLiteStorage {
	// Use temporary file for testing
	tmpFile := t.TempDir() + "/test.db"

	store, err := New(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create SQLite storage: %v", err)
	}

	// Run migrations
	ctx := context.Background()
	if err := store.RunMigrations(ctx); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	return store
}

func TestSQLiteStorage_SaveAndGetSubreddit(t *testing.T) {
	store := getTestDB(t)
	defer store.Close()

	ctx := context.Background()

	sub := &types.SubredditData{
		DisplayName: "golang",
		Title:       "The Go Programming Language",
		Description: "Ask questions and post articles about the Go programming language and related tools, events etc.",
		Subscribers: 250000,
	}

	// Save subreddit
	if err := store.SaveSubreddit(ctx, sub); err != nil {
		t.Fatalf("Failed to save subreddit: %v", err)
	}

	// Retrieve subreddit
	retrieved, err := store.GetSubreddit(ctx, "golang")
	if err != nil {
		t.Fatalf("Failed to get subreddit: %v", err)
	}

	if retrieved.DisplayName != sub.DisplayName {
		t.Errorf("Expected name %s, got %s", sub.DisplayName, retrieved.DisplayName)
	}

	if retrieved.Title != sub.Title {
		t.Errorf("Expected title %s, got %s", sub.Title, retrieved.Title)
	}
}

func TestSQLiteStorage_SaveAndGetPost(t *testing.T) {
	store := getTestDB(t)
	defer store.Close()

	ctx := context.Background()

	// Save subreddit first
	sub := &types.SubredditData{DisplayName: "golang"}
	if err := store.SaveSubreddit(ctx, sub); err != nil {
		t.Fatalf("Failed to save subreddit: %v", err)
	}

	post := &types.Post{
		ThingData: types.ThingData{
			ID:   "test123",
			Name: "t3_test123",
		},
		Created: types.Created{
			CreatedUTC: float64(time.Now().Unix()),
		},
		Subreddit:   "golang",
		Author:      "testuser",
		Title:       "Test Post Title",
		SelfText:    "This is a test post",
		URL:         "https://reddit.com/r/golang/comments/test123",
		Score:       42,
		NumComments: 10,
		IsSelf:      true,
	}

	// Save post
	if err := store.SavePost(ctx, post); err != nil {
		t.Fatalf("Failed to save post: %v", err)
	}

	// Retrieve post
	retrieved, err := store.GetPost(ctx, "test123")
	if err != nil {
		t.Fatalf("Failed to get post: %v", err)
	}

	if retrieved.ID != post.ID {
		t.Errorf("Expected ID %s, got %s", post.ID, retrieved.ID)
	}

	if retrieved.Title != post.Title {
		t.Errorf("Expected title %s, got %s", post.Title, retrieved.Title)
	}

	if retrieved.Score != post.Score {
		t.Errorf("Expected score %d, got %d", post.Score, retrieved.Score)
	}

	if retrieved.IsSelf != post.IsSelf {
		t.Errorf("Expected IsSelf %v, got %v", post.IsSelf, retrieved.IsSelf)
	}
}

func TestSQLiteStorage_SavePostsIdempotency(t *testing.T) {
	store := getTestDB(t)
	defer store.Close()

	ctx := context.Background()

	// Save subreddit first
	sub := &types.SubredditData{DisplayName: "golang"}
	if err := store.SaveSubreddit(ctx, sub); err != nil {
		t.Fatalf("Failed to save subreddit: %v", err)
	}

	post := &types.Post{
		ThingData: types.ThingData{
			ID:   "idempotent123",
			Name: "t3_idempotent123",
		},
		Created: types.Created{
			CreatedUTC: float64(time.Now().Unix()),
		},
		Subreddit:   "golang",
		Author:      "testuser",
		Title:       "Idempotency Test",
		Score:       10,
		NumComments: 5,
	}

	// Save post first time
	if err := store.SavePost(ctx, post); err != nil {
		t.Fatalf("Failed to save post first time: %v", err)
	}

	// Update post score
	post.Score = 20
	post.NumComments = 10

	// Save post second time (should update)
	if err := store.SavePost(ctx, post); err != nil {
		t.Fatalf("Failed to save post second time: %v", err)
	}

	// Retrieve and verify updated values
	retrieved, err := store.GetPost(ctx, "idempotent123")
	if err != nil {
		t.Fatalf("Failed to get post: %v", err)
	}

	if retrieved.Score != 20 {
		t.Errorf("Expected updated score 20, got %d", retrieved.Score)
	}

	if retrieved.NumComments != 10 {
		t.Errorf("Expected updated comment count 10, got %d", retrieved.NumComments)
	}
}

func TestSQLiteStorage_GetPostsBySubreddit(t *testing.T) {
	store := getTestDB(t)
	defer store.Close()

	ctx := context.Background()

	// Save subreddit
	sub := &types.SubredditData{DisplayName: "testsubreddit"}
	if err := store.SaveSubreddit(ctx, sub); err != nil {
		t.Fatalf("Failed to save subreddit: %v", err)
	}

	// Save multiple posts
	posts := []*types.Post{
		{
			ThingData: types.ThingData{ID: "post1", Name: "t3_post1"},
			Created:   types.Created{CreatedUTC: float64(time.Now().Add(-2 * time.Hour).Unix())},
			Subreddit: "testsubreddit",
			Title:     "Post 1",
			Score:     100,
		},
		{
			ThingData: types.ThingData{ID: "post2", Name: "t3_post2"},
			Created:   types.Created{CreatedUTC: float64(time.Now().Add(-1 * time.Hour).Unix())},
			Subreddit: "testsubreddit",
			Title:     "Post 2",
			Score:     50,
		},
		{
			ThingData: types.ThingData{ID: "post3", Name: "t3_post3"},
			Created:   types.Created{CreatedUTC: float64(time.Now().Unix())},
			Subreddit: "testsubreddit",
			Title:     "Post 3",
			Score:     200,
		},
	}

	if err := store.SavePosts(ctx, posts); err != nil {
		t.Fatalf("Failed to save posts: %v", err)
	}

	// Query posts sorted by score
	opts := storage.QueryOptions{
		Limit:     10,
		SortBy:    "score",
		SortOrder: "desc",
	}

	retrieved, err := store.GetPostsBySubreddit(ctx, "testsubreddit", opts)
	if err != nil {
		t.Fatalf("Failed to get posts: %v", err)
	}

	if len(retrieved) != 3 {
		t.Errorf("Expected 3 posts, got %d", len(retrieved))
	}

	// Verify sorting by score descending
	if len(retrieved) >= 2 {
		if retrieved[0].Score < retrieved[1].Score {
			t.Errorf("Posts not sorted by score descending: %d < %d", retrieved[0].Score, retrieved[1].Score)
		}
	}
}

func TestSQLiteStorage_GetPostsBySubreddit_DateFilters(t *testing.T) {
	store := getTestDB(t)
	defer store.Close()

	ctx := context.Background()

	// Save subreddit
	sub := &types.SubredditData{DisplayName: "daterange"}
	if err := store.SaveSubreddit(ctx, sub); err != nil {
		t.Fatalf("Failed to save subreddit: %v", err)
	}

	now := time.Now()
	older := now.Add(-48 * time.Hour)
	recent := now.Add(-1 * time.Hour)

	posts := []*types.Post{
		{
			ThingData: types.ThingData{ID: "old", Name: "t3_old"},
			Created:   types.Created{CreatedUTC: float64(older.Unix())},
			Subreddit: "daterange",
			Title:     "Old Post",
		},
		{
			ThingData: types.ThingData{ID: "new", Name: "t3_new"},
			Created:   types.Created{CreatedUTC: float64(recent.Unix())},
			Subreddit: "daterange",
			Title:     "New Post",
		},
	}

	if err := store.SavePosts(ctx, posts); err != nil {
		t.Fatalf("Failed to save posts: %v", err)
	}

	// Only the recent post should match the start date filter
	startOpts := storage.QueryOptions{
		StartDate: now.Add(-3 * time.Hour),
		SortBy:    "created",
		Limit:     10,
	}

	filtered, err := store.GetPostsBySubreddit(ctx, "daterange", startOpts)
	if err != nil {
		t.Fatalf("Failed to get posts with start date filter: %v", err)
	}

	if len(filtered) != 1 || filtered[0].ID != "new" {
		t.Fatalf("Expected only the recent post, got %+v", filtered)
	}

	// Only the older post should match the end date filter
	endOpts := storage.QueryOptions{
		EndDate:   now.Add(-24 * time.Hour),
		SortBy:    "created",
		SortOrder: "asc",
		Limit:     10,
	}

	filtered, err = store.GetPostsBySubreddit(ctx, "daterange", endOpts)
	if err != nil {
		t.Fatalf("Failed to get posts with end date filter: %v", err)
	}

	if len(filtered) != 1 || filtered[0].ID != "old" {
		t.Fatalf("Expected only the older post, got %+v", filtered)
	}
}

func TestSQLiteStorage_GetPostStats_NoComments(t *testing.T) {
	store := getTestDB(t)
	defer store.Close()

	ctx := context.Background()

	sub := &types.SubredditData{DisplayName: "stats"}
	if err := store.SaveSubreddit(ctx, sub); err != nil {
		t.Fatalf("Failed to save subreddit: %v", err)
	}

	post := &types.Post{
		ThingData: types.ThingData{ID: "statspost", Name: "t3_statspost"},
		Created:   types.Created{CreatedUTC: float64(time.Now().Unix())},
		Subreddit: "stats",
		Title:     "Stats Post",
	}

	if err := store.SavePost(ctx, post); err != nil {
		t.Fatalf("Failed to save post: %v", err)
	}

	stats, err := store.GetPostStats(ctx, "statspost")
	if err != nil {
		t.Fatalf("Failed to get post stats: %v", err)
	}

	if stats.CommentCount != 0 {
		t.Fatalf("Expected zero comments, got %d", stats.CommentCount)
	}

	if stats.MaxCommentDepth != 0 {
		t.Fatalf("Expected zero max depth, got %d", stats.MaxCommentDepth)
	}
}

func TestSQLiteStorage_SaveAndGetComments(t *testing.T) {
	store := getTestDB(t)
	defer store.Close()

	ctx := context.Background()

	// Setup subreddit and post
	sub := &types.SubredditData{DisplayName: "golang"}
	if err := store.SaveSubreddit(ctx, sub); err != nil {
		t.Fatalf("Failed to save subreddit: %v", err)
	}

	post := &types.Post{
		ThingData: types.ThingData{ID: "post_with_comments", Name: "t3_post_with_comments"},
		Created:   types.Created{CreatedUTC: float64(time.Now().Unix())},
		Subreddit: "golang",
		Title:     "Post with Comments",
	}

	if err := store.SavePost(ctx, post); err != nil {
		t.Fatalf("Failed to save post: %v", err)
	}

	// Create comments
	comments := []*types.Comment{
		{
			ThingData: types.ThingData{ID: "comment1", Name: "t1_comment1"},
			Created:   types.Created{CreatedUTC: float64(time.Now().Unix())},
			LinkID:    "t3_post_with_comments",
			Author:    "user1",
			Body:      "Top level comment",
			Score:     10,
		},
		{
			ThingData: types.ThingData{ID: "comment2", Name: "t1_comment2"},
			Created:   types.Created{CreatedUTC: float64(time.Now().Add(1 * time.Minute).Unix())},
			LinkID:    "t3_post_with_comments",
			ParentID:  "t1_comment1",
			Author:    "user2",
			Body:      "Reply to comment1",
			Score:     5,
		},
	}

	if err := store.SaveComments(ctx, comments); err != nil {
		t.Fatalf("Failed to save comments: %v", err)
	}

	// Retrieve comments
	retrieved, err := store.GetCommentsByPost(ctx, "post_with_comments")
	if err != nil {
		t.Fatalf("Failed to get comments: %v", err)
	}

	if len(retrieved) != 2 {
		t.Errorf("Expected 2 comments, got %d", len(retrieved))
	}
}

func TestSQLiteStorage_Migrations(t *testing.T) {
	tmpFile := t.TempDir() + "/migrations_test.db"

	store, err := New(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create SQLite storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Run migrations
	if err := store.RunMigrations(ctx); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Run migrations again (should be idempotent)
	if err := store.RunMigrations(ctx); err != nil {
		t.Fatalf("Failed to run migrations second time: %v", err)
	}
}

func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()
	os.Exit(code)
}
