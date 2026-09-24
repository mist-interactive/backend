/*
  Create comments table for profile wall messages
*/

CREATE TABLE comments (
    id BIGSERIAL PRIMARY KEY,
    owner_id BIGINT NOT NULL REFERENCES users(id),
    poster_id BIGINT NOT NULL REFERENCES users(id),
    content VARCHAR(1000) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_comments_to_owner ON comments(owner_id, created_at DESC, id DESC);
CREATE INDEX idx_comments_poster ON comments(poster_id);
