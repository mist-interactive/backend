/*
  Create table `friendships` to keep track of riends requests and existing relationships
  Create indexes for both sides of the friendship id
*/

CREATE TABLE IF NOT EXISTS friendships (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  friend_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'blocked')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

  -- Prevent friending oneself
  CONSTRAINT check_not_self CHECK (user_id <> friend_id)
);

--Prevent duplicate friendships: create a unique index of the ordered user_id,friend_id pair
-- if 1,2 are already friends, trying to insert 2,1 will cause a unique violation when the index updates
CREATE UNIQUE INDEX unique_friendship_pair ON friendships (
    LEAST(user_id, friend_id),
    GREATEST(user_id, friend_id)
);
CREATE INDEX IF NOT EXISTS idx_friendships_user_id ON friendships(user_id);
CREATE INDEX IF NOT EXISTS idx_friendships_friend_id ON friendships(friend_id);
