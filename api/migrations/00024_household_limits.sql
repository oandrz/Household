-- +goose Up
-- Per-household limits (ADR 13). Every create counts the household's rows
-- of one kind before writing, so each count needs an index that starts with
-- household_id; without one the count reads every household's rows. Tables
-- whose existing unique key already starts with household_id (categories,
-- goals, bills, spaces, agreement_sections, memberships, retros) need none.
CREATE INDEX accounts_household_all_idx    ON accounts (household_id);
CREATE INDEX invites_household_idx         ON invites (household_id);
CREATE INDEX api_tokens_household_idx      ON api_tokens (household_id);
CREATE INDEX holding_events_household_idx  ON holding_events (household_id);
CREATE INDEX holding_valuations_household_idx ON holding_valuations (household_id);

-- Text limits, counted in characters exactly as the domain counts them
-- (domain/text_limits.go). The domain refuses first with a readable
-- message; these are the second line, for a write path that forgets.
ALTER TABLE households ADD CONSTRAINT households_name_length CHECK (char_length(name) <= 80);
ALTER TABLE households ADD CONSTRAINT households_family_name_length CHECK (char_length(family_name) <= 80);
ALTER TABLE users ADD CONSTRAINT users_display_name_length CHECK (char_length(display_name) <= 80);
ALTER TABLE invites ADD CONSTRAINT invites_name_length CHECK (char_length(name) <= 80);
ALTER TABLE spaces ADD CONSTRAINT spaces_name_length CHECK (char_length(name) <= 80);
ALTER TABLE accounts ADD CONSTRAINT accounts_nickname_length CHECK (char_length(nickname) <= 80);
ALTER TABLE categories ADD CONSTRAINT categories_name_length CHECK (char_length(name) <= 80);
ALTER TABLE goals ADD CONSTRAINT goals_name_length CHECK (char_length(name) <= 80);
ALTER TABLE bills ADD CONSTRAINT bills_name_length CHECK (char_length(name) <= 80);
ALTER TABLE holdings ADD CONSTRAINT holdings_name_length CHECK (char_length(name) <= 80);
ALTER TABLE holdings ADD CONSTRAINT holdings_unit_length CHECK (char_length(unit) <= 20);
ALTER TABLE transactions ADD CONSTRAINT transactions_description_length CHECK (char_length(description) <= 200);
ALTER TABLE goal_contributions ADD CONSTRAINT goal_contributions_note_length CHECK (char_length(note) <= 500);
ALTER TABLE holding_events ADD CONSTRAINT holding_events_note_length CHECK (char_length(note) <= 500);
ALTER TABLE holding_income ADD CONSTRAINT holding_income_note_length CHECK (char_length(note) <= 500);
ALTER TABLE retro_actions ADD CONSTRAINT retro_actions_body_length CHECK (char_length(body) <= 500);

-- +goose Down
ALTER TABLE retro_actions DROP CONSTRAINT retro_actions_body_length;
ALTER TABLE holding_income DROP CONSTRAINT holding_income_note_length;
ALTER TABLE holding_events DROP CONSTRAINT holding_events_note_length;
ALTER TABLE goal_contributions DROP CONSTRAINT goal_contributions_note_length;
ALTER TABLE transactions DROP CONSTRAINT transactions_description_length;
ALTER TABLE holdings DROP CONSTRAINT holdings_unit_length;
ALTER TABLE holdings DROP CONSTRAINT holdings_name_length;
ALTER TABLE bills DROP CONSTRAINT bills_name_length;
ALTER TABLE goals DROP CONSTRAINT goals_name_length;
ALTER TABLE categories DROP CONSTRAINT categories_name_length;
ALTER TABLE accounts DROP CONSTRAINT accounts_nickname_length;
ALTER TABLE spaces DROP CONSTRAINT spaces_name_length;
ALTER TABLE invites DROP CONSTRAINT invites_name_length;
ALTER TABLE users DROP CONSTRAINT users_display_name_length;
ALTER TABLE households DROP CONSTRAINT households_family_name_length;
ALTER TABLE households DROP CONSTRAINT households_name_length;
DROP INDEX holding_valuations_household_idx;
DROP INDEX holding_events_household_idx;
DROP INDEX api_tokens_household_idx;
DROP INDEX invites_household_idx;
DROP INDEX accounts_household_all_idx;
