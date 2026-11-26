-- 0004_self_registration.sql
-- Adds the approval axis, the account origin and the guild-verification stamp.
--
-- ADD COLUMN with a constant default is a metadata-only change in SQLite: there
-- is no table rebuild and no session cascade, so nobody is signed out. Every
-- existing account and every administrator-created account becomes 'Approved',
-- which is exactly today's behaviour.
--
-- DisabledFg stays the administrative penalty flag; AccountStatus answers a
-- different question ("has an officer admitted this account?"). The CHECK keeps
-- a corrupt value out of the table; ParseAccountStatus still fails closed when
-- reading an older row that predates the CHECK.
--
-- Origin is 'SelfRegistration' or NULL (administrator-created).
-- GuildVerifiedAt is the last successful Discord guild check, UTC ISO-8601.

ALTER TABLE Users ADD COLUMN AccountStatus TEXT NOT NULL DEFAULT 'Approved'
    CHECK (AccountStatus IN ('Approved','PendingApproval'));

ALTER TABLE Users ADD COLUMN Origin          TEXT NULL;
ALTER TABLE Users ADD COLUMN GuildVerifiedAt TEXT NULL;

CREATE INDEX ix_users_status ON Users (AccountStatus, CreatedAt);
