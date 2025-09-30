package testutil

import (
	"time"

	"github.com/jamesprial/go-reddit-api-wrapper/pkg/types"
)

// NewTestPost creates a test post with proper embedded types
func NewTestPost(id, subreddit, title string) *types.Post {
	return &types.Post{
		ThingData: types.ThingData{
			ID:   id,
			Name: "t3_" + id,
		},
		Created: types.Created{
			CreatedUTC: float64(time.Now().Unix()),
		},
		Subreddit:   subreddit,
		Title:       title,
		NumComments: 0,
		Score:       0,
	}
}

// NewTestComment creates a test comment with proper embedded types
func NewTestComment(id, postID, author, body string) *types.Comment {
	return &types.Comment{
		ThingData: types.ThingData{
			ID:   id,
			Name: "t1_" + id,
		},
		Created: types.Created{
			CreatedUTC: float64(time.Now().Unix()),
		},
		LinkID: "t3_" + postID,
		Author: author,
		Body:   body,
		Score:  0,
	}
}