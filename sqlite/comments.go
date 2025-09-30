package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jamesprial/go-reddit-api-wrapper/pkg/types"
	"github.com/jamesprial/go-reddit-storage"
)

// SaveComment saves or updates a single comment
func (s *SQLiteStorage) SaveComment(ctx context.Context, comment *types.Comment) error {
	rawJSON, err := json.Marshal(comment)
	if err != nil {
		return &storage.StorageError{Op: "marshal_comment", Err: err}
	}

	query := `
		INSERT INTO comments (
			id, post_id, parent_id, author, body, score,
			depth, created_utc, edited_utc, raw_json, last_updated
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP
		)
		ON CONFLICT (id) DO UPDATE SET
			score = excluded.score,
			body = excluded.body,
			edited_utc = excluded.edited_utc,
			last_updated = CURRENT_TIMESTAMP,
			raw_json = excluded.raw_json
	`

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

	// Calculate depth
	depth := 0
	if parentID != nil {
		depth = 1
	}

	// Handle edited timestamp
	var editedUTC interface{}
	if comment.Edited.IsEdited && comment.Edited.Timestamp > 0 {
		editedUTC = comment.Edited.Timestamp
	}

	_, err = s.db.ExecContext(ctx, query,
		comment.ID, postID, parentID, comment.Author,
		comment.Body, comment.Score, depth, comment.CreatedUTC,
		editedUTC, string(rawJSON),
	)

	if err != nil {
		return &storage.StorageError{Op: "save_comment", Err: err}
	}

	return nil
}

// SaveComments saves or updates multiple comments in a transaction
func (s *SQLiteStorage) SaveComments(ctx context.Context, comments []*types.Comment) error {
	if len(comments) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return &storage.StorageError{Op: "begin_transaction", Err: err}
	}
	defer tx.Rollback()

	query := `
		INSERT INTO comments (
			id, post_id, parent_id, author, body, score,
			depth, created_utc, edited_utc, raw_json, last_updated
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP
		)
		ON CONFLICT (id) DO UPDATE SET
			score = excluded.score,
			body = excluded.body,
			edited_utc = excluded.edited_utc,
			last_updated = CURRENT_TIMESTAMP,
			raw_json = excluded.raw_json
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

		// Calculate depth
		depth := 0
		if parentID != nil {
			depth = 1
		}

		// Handle edited timestamp
		var editedUTC interface{}
		if comment.Edited.IsEdited && comment.Edited.Timestamp > 0 {
			editedUTC = comment.Edited.Timestamp
		}

		_, err = stmt.ExecContext(ctx,
			comment.ID, postID, parentID, comment.Author,
			comment.Body, comment.Score, depth, comment.CreatedUTC,
			editedUTC, string(rawJSON),
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
func (s *SQLiteStorage) GetCommentsByPost(ctx context.Context, postID string) ([]*types.Comment, error) {
	query := `
		WITH RECURSIVE comment_tree AS (
			-- Top-level comments
			SELECT id, post_id, parent_id, author, body, score, depth,
			       created_utc, edited_utc, raw_json, 0 as level,
			       created_utc as path
			FROM comments
			WHERE post_id = ? AND parent_id IS NULL

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
		var rawJSON string
		var parentID sql.NullString
		var postIDRaw string
		var depth int
		var editedUTC sql.NullString

		err := rows.Scan(
			&comment.ID, &postIDRaw, &parentID, &comment.Author,
			&comment.Body, &comment.Score, &depth, &comment.CreatedUTC,
			&editedUTC, &rawJSON,
		)

		if err != nil {
			return nil, &storage.StorageError{Op: "scan_comment", Err: err}
		}

		// Reconstruct fullnames with prefixes
		comment.LinkID = "t3_" + postIDRaw

		if parentID.Valid {
			comment.ParentID = "t1_" + parentID.String
		} else {
			comment.ParentID = comment.LinkID
		}

		// Reconstruct Edited field
		if editedUTC.Valid {
			// Try to parse as float64
			var timestamp float64
			if _, err := fmt.Sscanf(editedUTC.String, "%f", &timestamp); err == nil {
				comment.Edited = types.Edited{IsEdited: true, Timestamp: timestamp}
			} else {
				comment.Edited = types.Edited{IsEdited: false}
			}
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