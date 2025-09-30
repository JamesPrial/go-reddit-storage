package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/jamesprial/go-reddit-api-wrapper/pkg/types"
	"github.com/jamesprial/go-reddit-storage"
)

// SaveComment saves or updates a single comment
func (s *PostgresStorage) SaveComment(ctx context.Context, comment *types.Comment) error {
	rawJSON, err := json.Marshal(comment)
	if err != nil {
		return &storage.StorageError{Op: "marshal_comment", Err: err}
	}

	query := `
		INSERT INTO comments (
			id, post_id, parent_id, author, body, score,
			depth, created_utc, edited_utc, raw_json, last_updated
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW()
		)
		ON CONFLICT (id) DO UPDATE SET
			score = EXCLUDED.score,
			body = EXCLUDED.body,
			edited_utc = EXCLUDED.edited_utc,
			last_updated = NOW(),
			raw_json = EXCLUDED.raw_json
	`

	// Handle NULL parent_id for top-level comments
	// ParentID is the fullname (e.g., "t3_postid" or "t1_commentid")
	var parentID interface{}
	postID := comment.LinkID // LinkID is the post fullname (e.g., "t3_abc123")

	if comment.ParentID == "" || comment.ParentID == postID {
		parentID = nil
	} else {
		// Strip the "t1_" prefix from comment parent IDs for storage
		if len(comment.ParentID) > 3 {
			parentID = comment.ParentID[3:]
		} else {
			parentID = comment.ParentID
		}
	}

	// Strip "t3_" prefix from LinkID for post_id
	if len(postID) > 3 {
		postID = postID[3:]
	}

	// Calculate depth by querying parent if it exists
	depth := 0
	if parentID != nil {
		var parentDepth sql.NullInt64
		err := s.db.QueryRowContext(ctx, "SELECT depth FROM comments WHERE id = $1", parentID).Scan(&parentDepth)
		if err == nil && parentDepth.Valid {
			depth = int(parentDepth.Int64) + 1
		} else {
			// If parent not found, assume depth 1 (direct reply to post)
			depth = 1
		}
	}

	createdAt, _ := unixFloatToTime(comment.CreatedUTC)
	editedAt, hasEdited := unixFloatToTime(comment.Edited.Timestamp)
	if !comment.Edited.IsEdited {
		hasEdited = false
	}

	_, err = s.db.ExecContext(ctx, query,
		comment.ID, postID, parentID, comment.Author,
		comment.Body, comment.Score, depth, createdAt,
		timePtrOrNil(editedAt, hasEdited), rawJSON,
	)

	if err != nil {
		return &storage.StorageError{Op: "save_comment", Err: err}
	}

	return nil
}

// SaveComments saves or updates multiple comments in a transaction
func (s *PostgresStorage) SaveComments(ctx context.Context, comments []*types.Comment) error {
	if len(comments) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return &storage.StorageError{Op: "begin_transaction", Err: err}
	}
	defer tx.Rollback()

	// Build a map of comment ID to parent ID for depth calculation
	commentMap := make(map[string]string) // commentID -> parentID (stripped)
	for _, comment := range comments {
		var parentID string
		if comment.ParentID != "" && comment.ParentID != comment.LinkID {
			// Strip "t1_" prefix from parent comment IDs
			if len(comment.ParentID) > 3 && comment.ParentID[:3] == "t1_" {
				parentID = comment.ParentID[3:]
			} else {
				parentID = comment.ParentID
			}
		}
		commentMap[comment.ID] = parentID
	}

	// Function to calculate depth by recursively following parent chain
	depthCache := make(map[string]int)
	var calculateDepth func(commentID string) int
	calculateDepth = func(commentID string) int {
		// Check cache first
		if depth, ok := depthCache[commentID]; ok {
			return depth
		}

		parentID, exists := commentMap[commentID]
		if !exists || parentID == "" {
			// Top-level comment or parent not in this batch
			// Query database for parent depth if parent exists
			if parentID != "" {
				var parentDepth sql.NullInt64
				err := tx.QueryRowContext(ctx, "SELECT depth FROM comments WHERE id = $1", parentID).Scan(&parentDepth)
				if err == nil && parentDepth.Valid {
					depth := int(parentDepth.Int64) + 1
					depthCache[commentID] = depth
					return depth
				}
			}
			// Assume top-level if parent not found
			depthCache[commentID] = 0
			return 0
		}

		// Parent is in this batch, calculate recursively
		depth := calculateDepth(parentID) + 1
		depthCache[commentID] = depth
		return depth
	}

	query := `
		INSERT INTO comments (
			id, post_id, parent_id, author, body, score,
			depth, created_utc, edited_utc, raw_json, last_updated
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW()
		)
		ON CONFLICT (id) DO UPDATE SET
			score = EXCLUDED.score,
			body = EXCLUDED.body,
			edited_utc = EXCLUDED.edited_utc,
			depth = EXCLUDED.depth,
			last_updated = NOW(),
			raw_json = EXCLUDED.raw_json
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return &storage.StorageError{Op: "prepare_statement", Err: err}
	}
	defer stmt.Close()

	for _, comment := range comments {
		rawJSON, err := json.Marshal(comment)
		if err != nil {
			return &storage.StorageError{Op: "marshal_comment", Err: err}
		}

		// Handle NULL parent_id for top-level comments
		var parentID interface{}
		postID := comment.LinkID

		if comment.ParentID == "" || comment.ParentID == postID {
			parentID = nil
		} else {
			// Strip the "t1_" prefix from comment parent IDs
			if len(comment.ParentID) > 3 {
				parentID = comment.ParentID[3:]
			} else {
				parentID = comment.ParentID
			}
		}

		// Strip "t3_" prefix from LinkID for post_id
		if len(postID) > 3 {
			postID = postID[3:]
		}

		// Calculate proper depth
		depth := calculateDepth(comment.ID)

		createdAt, _ := unixFloatToTime(comment.CreatedUTC)
		editedAt, hasEdited := unixFloatToTime(comment.Edited.Timestamp)
		if !comment.Edited.IsEdited {
			hasEdited = false
		}

		_, err = stmt.ExecContext(ctx,
			comment.ID, postID, parentID, comment.Author,
			comment.Body, comment.Score, depth, createdAt,
			timePtrOrNil(editedAt, hasEdited), rawJSON,
		)

		if err != nil {
			return &storage.StorageError{Op: "insert_comment", Err: err}
		}
	}

	if err := tx.Commit(); err != nil {
		return &storage.StorageError{Op: "commit_transaction", Err: err}
	}

	return nil
}

// GetCommentsByPost retrieves all comments for a post, preserving thread structure
func (s *PostgresStorage) GetCommentsByPost(ctx context.Context, postID string) ([]*types.Comment, error) {
	query := `
		WITH RECURSIVE comment_tree AS (
			-- Top-level comments
			SELECT id, post_id, parent_id, author, body, score, depth,
			       created_utc, edited_utc, raw_json, 0 as level,
			       ARRAY[created_utc] as path
			FROM comments
			WHERE post_id = $1 AND parent_id IS NULL

			UNION ALL

			-- Nested comments
			SELECT c.id, c.post_id, c.parent_id, c.author, c.body, c.score,
			       c.depth, c.created_utc, c.edited_utc, c.raw_json,
			       ct.level + 1,
			       ct.path || c.created_utc
			FROM comments c
			JOIN comment_tree ct ON c.parent_id = ct.id
		)
		SELECT id, post_id, parent_id, author, body, score, depth,
		       created_utc, edited_utc, raw_json
		FROM comment_tree
		ORDER BY path
	`

	rows, err := s.db.QueryContext(ctx, query, postID)
	if err != nil {
		return nil, &storage.StorageError{Op: "get_comments_by_post", Err: err}
	}
	defer rows.Close()

	var comments []*types.Comment

	for rows.Next() {
		var comment types.Comment
		var rawJSON []byte
		var parentID sql.NullString

		var postIDRaw string
		var depth int
		var createdAt time.Time
		var editedUTC sql.NullTime

		err := rows.Scan(
			&comment.ID, &postIDRaw, &parentID, &comment.Author,
			&comment.Body, &comment.Score, &depth, &createdAt,
			&editedUTC, &rawJSON,
		)

		if err != nil {
			return nil, &storage.StorageError{Op: "scan_comment", Err: err}
		}

		comment.CreatedUTC = timeToUnixFloat(createdAt)

		// Reconstruct fullnames with prefixes
		comment.LinkID = "t3_" + postIDRaw

		if parentID.Valid {
			comment.ParentID = "t1_" + parentID.String
		} else {
			comment.ParentID = comment.LinkID // Top-level comments have post as parent
		}

		// Reconstruct Edited field
		if editedUTC.Valid {
			comment.Edited = types.Edited{IsEdited: true, Timestamp: timeToUnixFloat(editedUTC.Time)}
		} else {
			comment.Edited = types.Edited{IsEdited: false}
		}

		comments = append(comments, &comment)
	}

	if err := rows.Err(); err != nil {
		return nil, &storage.StorageError{Op: "scan_comments", Err: err}
	}

	return comments, nil
}
