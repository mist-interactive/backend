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
    result VARCHAR(16) NOT NULL,
    started_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMP WITH TIME ZONE,

    CONSTRAINT fk_matches_player_one FOREIGN KEY (player_one) REFERENCES users(id),
    CONSTRAINT fk_matches_player_two FOREIGN KEY (player_two) REFERENCES users(id),
    CONSTRAINT check_match_result CHECK(result IN ('player1_win', 'player2_win', 'draw', 'aborted'))
);

CREATE INDEX idx_matches_player_one ON matches(player_one);
CREATE INDEX idx_matches_player_two ON matches(player_two);
