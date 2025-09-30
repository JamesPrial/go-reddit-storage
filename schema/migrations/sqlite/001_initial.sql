-- Subreddits table
CREATE TABLE IF NOT EXISTS subreddits (
    name TEXT PRIMARY KEY,
    display_name TEXT,
    title TEXT,
    description TEXT,
    subscribers INTEGER,
    created_utc TEXT,
    last_synced TEXT DEFAULT CURRENT_TIMESTAMP,
    raw_json TEXT
);

-- Posts table
CREATE TABLE IF NOT EXISTS posts (
    id TEXT PRIMARY KEY,
    subreddit TEXT NOT NULL,
    author TEXT,
    title TEXT NOT NULL,
    selftext TEXT,
    url TEXT,
    score INTEGER DEFAULT 0,
    upvote_ratio REAL,
    num_comments INTEGER DEFAULT 0,
    created_utc TEXT NOT NULL,
    edited_utc TEXT,
    is_self INTEGER DEFAULT 0,
    is_video INTEGER DEFAULT 0,
    archived_at TEXT DEFAULT CURRENT_TIMESTAMP,
    last_updated TEXT DEFAULT CURRENT_TIMESTAMP,
    raw_json TEXT,
    FOREIGN KEY (subreddit) REFERENCES subreddits(name) ON DELETE CASCADE
);

-- Comments table
CREATE TABLE IF NOT EXISTS comments (
    id TEXT PRIMARY KEY,
    post_id TEXT NOT NULL,
    parent_id TEXT,
    author TEXT,
    body TEXT,
    score INTEGER DEFAULT 0,
    depth INTEGER DEFAULT 0,
    created_utc TEXT NOT NULL,
    edited_utc TEXT,
    archived_at TEXT DEFAULT CURRENT_TIMESTAMP,
    last_updated TEXT DEFAULT CURRENT_TIMESTAMP,
    raw_json TEXT,
    FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE,
    FOREIGN KEY (parent_id) REFERENCES comments(id) ON DELETE CASCADE
);

-- Archive metadata for tracking sync state
CREATE TABLE IF NOT EXISTS archive_metadata (
    subreddit TEXT PRIMARY KEY,
    last_post_id TEXT,
    last_sync TEXT,
    total_posts INTEGER DEFAULT 0,
    total_comments INTEGER DEFAULT 0
);