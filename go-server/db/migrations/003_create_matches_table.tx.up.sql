/*
  setting up the database:
  creates table
    matches
  creates indexes for faster lookup for
    matches(player_one)
    matches(player_two)
*/

CREATE TABLE IF NOT EXISTS matches (
    id BIGSERIAL PRIMARY KEY,
    player_one BIGINT NOT NULL,
    player_two BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'in_progress',
    result VARCHAR(16),
    started_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMP WITH TIME ZONE,

    CONSTRAINT fk_matches_player_one FOREIGN KEY (player_one) REFERENCES users(id),
    CONSTRAINT fk_matches_player_two FOREIGN KEY (player_two) REFERENCES users(id),
    CONSTRAINT check_match_result CHECK(result IN ('player1_win', 'player2_win', 'draw', 'aborted')),
    CONSTRAINT check_match_status CHECK(status IN ('in_progress', 'finished', 'abandoned')),
    CONSTRAINT check_different_players CHECK (player_one <> player_two)
);

CREATE INDEX idx_matches_player_one ON matches(player_one, started_at DESC);
CREATE INDEX idx_matches_player_two ON matches(player_two, started_at DESC);
