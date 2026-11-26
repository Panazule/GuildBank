-- 0003_user_profiles.sql
-- Adds the Discord identity columns and the per-account character table.
--
-- ADD COLUMN on a nullable column without a default is metadata-only in SQLite,
-- so unlike 0002 there is no table rebuild, no session cascade and no forced
-- sign-in. The Discord columns stay inert until the linking phase ships; the
-- character table is used immediately.
--
-- Character names are unique across the whole installation (owner decision D1):
-- the request workflow uses them as identity, so two accounts must not be able
-- to hold the same name. COLLATE NOCASE matches the ASCII name alphabet.
--
-- The partial unique index keeps unlinked accounts (DiscordUserID IS NULL) from
-- colliding with each other.

ALTER TABLE Users ADD COLUMN DiscordUserID   TEXT NULL;
ALTER TABLE Users ADD COLUMN DiscordUserName TEXT NULL;
ALTER TABLE Users ADD COLUMN DiscordLinkedAt TEXT NULL;

CREATE UNIQUE INDEX ux_users_discord
    ON Users (DiscordUserID)
    WHERE DiscordUserID IS NOT NULL;

CREATE TABLE UserCharacters (
    CharacterID   INTEGER PRIMARY KEY AUTOINCREMENT,
    UserID        INTEGER NOT NULL
                  REFERENCES Users(UserID) ON DELETE CASCADE,
    CharacterName TEXT    NOT NULL,
    CreatedAt     TEXT    NOT NULL,
    CreatedBy     TEXT    NULL
);

CREATE UNIQUE INDEX ux_user_characters_name
    ON UserCharacters (CharacterName COLLATE NOCASE);

CREATE INDEX ix_user_characters_user ON UserCharacters (UserID);
