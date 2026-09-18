/*
  Alter table matches to include heartbeat timestamp
*/

ALTER TABLE matches
ADD COLUMN last_heartbeat_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP;
