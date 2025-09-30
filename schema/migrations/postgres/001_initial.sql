-- Subreddits table
CREATE TABLE IF NOT EXISTS subreddits (
    name TEXT PRIMARY KEY,
    display_name TEXT,
    title TEXT,
    description TEXT,
    subscribers INTEGER,
    created_utc TIMESTAMP,
    last_synced TIMESTAMP DEFAULT NOW(),
    raw_json JSONB
);

-- Posts table
CREATE TABLE IF NOT EXISTS posts (
    id TEXT PRIMARY KEY,
    subreddit TEXT NOT NULL REFERENCES subreddits(name),
    author TEXT,
    title TEXT NOT NULL,
    selftext TEXT,
    url TEXT,
    score INTEGER DEFAULT 0,
    upvote_ratio REAL,
    num_comments INTEGER DEFAULT 0,
    created_utc TIMESTAMP NOT NULL,
    edited_utc TIMESTAMP,
    is_self BOOLEAN DEFAULT false,
    is_video BOOLEAN DEFAULT false,
    archived_at TIMESTAMP DEFAULT NOW(),
    last_updated TIMESTAMP DEFAULT NOW(),
    raw_json JSONB,
    CONSTRAINT posts_subreddit_fkey FOREIGN KEY (subreddit)
        REFERENCES subreddits(name) ON DELETE CASCADE
);

-- Comments table
CREATE TABLE IF NOT EXISTS comments (
    id TEXT PRIMARY KEY,
    post_id TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    parent_id TEXT,
    author TEXT,
    body TEXT,
    score INTEGER DEFAULT 0,
    depth INTEGER DEFAULT 0,
    created_utc TIMESTAMP NOT NULL,
    edited_utc TIMESTAMP,
    archived_at TIMESTAMP DEFAULT NOW(),
    last_updated TIMESTAMP DEFAULT NOW(),
    raw_json JSONB,
    CONSTRAINT comments_parent_fkey FOREIGN KEY (parent_id)
        REFERENCES comments(id) ON DELETE CASCADE
);

-- Archive metadata for tracking sync state
CREATE TABLE IF NOT EXISTS archive_metadata (
    subreddit TEXT PRIMARY KEY,
    last_post_id TEXT,
    last_sync TIMESTAMP,
    total_posts INTEGER DEFAULT 0,
    total_comments INTEGER DEFAULT 0
);