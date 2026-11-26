-- 0002_user_roles.sql
-- Widens Users.AccessLevel to the approved Member/Officer/Admin hierarchy and
-- makes Member the least-privileged default. SQLite cannot alter a CHECK in
-- place, so the table is rebuilt.
--
-- Sessions.UserID references Users ON DELETE CASCADE, and every connection runs
-- PRAGMA foreign_keys=ON, so the DROP below removes every session. That is the
-- approved behaviour: accounts sign in again after the upgrade. Passwords,
-- names, flags and timestamps are copied verbatim.
--
-- sqlite_sequence is normalised explicitly because the table is AUTOINCREMENT:
-- without it the renamed table can continue from a stale or missing high-water
-- mark and collide with an existing UserID.

CREATE TABLE Users_new (
    UserID             INTEGER PRIMARY KEY AUTOINCREMENT,
    UserName           TEXT    NOT NULL COLLATE NOCASE,
    PasswordHash       TEXT    NOT NULL,
    AccessLevel        TEXT    NOT NULL DEFAULT 'Member'
                               CHECK (AccessLevel IN ('Admin','Officer','Member')),
    LastLogin          TEXT    NULL,
    DisabledFg         INTEGER NOT NULL DEFAULT 0
                               CHECK (DisabledFg IN (0,1)),
    ModAt              TEXT    NOT NULL,
    CreatedAt          TEXT    NOT NULL,
    ModBy              TEXT    NULL,
    MustChangePassword INTEGER NOT NULL DEFAULT 0
                               CHECK (MustChangePassword IN (0,1))
);

INSERT INTO Users_new (UserID, UserName, PasswordHash, AccessLevel, LastLogin,
                       DisabledFg, ModAt, CreatedAt, ModBy, MustChangePassword)
SELECT UserID, UserName, PasswordHash, AccessLevel, LastLogin,
       DisabledFg, ModAt, CreatedAt, ModBy, MustChangePassword
FROM Users;

DROP TABLE Users;

ALTER TABLE Users_new RENAME TO Users;

CREATE UNIQUE INDEX ux_users_username ON Users (UserName COLLATE NOCASE);

DELETE FROM sqlite_sequence WHERE name IN ('Users', 'Users_new');

INSERT INTO sqlite_sequence (name, seq)
SELECT 'Users', COALESCE(MAX(UserID), 0) FROM Users;
