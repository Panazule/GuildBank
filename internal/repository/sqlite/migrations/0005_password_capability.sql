-- 0005_password_capability.sql
-- Records whether an account has ever established a password (D-SR-15/16).
--
-- ADD COLUMN with a constant default is metadata-only in SQLite: there is no
-- table rebuild and no session cascade, so nobody is signed out. The column is
-- read through userColumns/scanUser and sessionSelect/scanSession, so a session
-- read sees the same value as an account read.
--
-- HasPassword is a capability flag, never a credential: PasswordHash stays the
-- only verifier, and a false value only narrows authentication.
--
-- Backfill (D-SR-21). Every row that exists before this migration defaults to 1.
-- That is exact for administrator-created accounts and for the setup
-- administrator, which are given a password. Self-registered accounts were
-- created with a discarded bcrypt hash of random bytes and have no password the
-- user knows, so they are set to 0. Origin = 'SelfRegistration' is the best
-- evidence available; a self-registered account that had already set a password
-- before this migration would be marked 0 and asked to set one again. That is a
-- one-time, harmless approximation: it can only narrow, and it is corrected the
-- next time the owner sets a password. The opposite error is impossible.
--
-- The transition is monotone: SetOwnPassword and ResetUserPassword write 1 and
-- nothing writes 0 after this migration.

ALTER TABLE Users ADD COLUMN HasPassword INTEGER NOT NULL DEFAULT 1
    CHECK (HasPassword IN (0,1));

UPDATE Users SET HasPassword = 0 WHERE Origin = 'SelfRegistration';
