/*
  Alter table users to contain more fields needed by the profile endpoints
*/

ALTER TABLE users
ADD COLUMN bio TEXT NOT NULL DEFAULT '',
ADD COLUMN avatar_url VARCHAR(512) NULL,
ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;
