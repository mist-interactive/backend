ALTER TABLE users ADD COLUMN total_wins INT DEFAULT 0;
ALTER TABLE users ADD COLUMN total_losses INT DEFAULT 0;
ALTER TABLE users ADD COLUMN avatar_url TEXT DEFAULT 'default.png';
