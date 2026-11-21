-- Phase 1 SQLite schema. Forward-only; the runner sets PRAGMA user_version.
-- Users, Settings and Sessions are the only objects required for setup,
-- authentication and configuration persistence. AuditOutbox is deferred with
-- audit logging (owner Phase 1 scope).

CREATE TABLE Users (
    UserID             INTEGER PRIMARY KEY AUTOINCREMENT,
    UserName           TEXT    NOT NULL COLLATE NOCASE,
    PasswordHash       TEXT    NOT NULL,
    AccessLevel        TEXT    NOT NULL DEFAULT 'Admin'
                               CHECK (AccessLevel IN ('Admin')),
    LastLogin          TEXT    NULL,
    DisabledFg         INTEGER NOT NULL DEFAULT 0
                               CHECK (DisabledFg IN (0,1)),
    ModAt              TEXT    NOT NULL,
    CreatedAt          TEXT    NOT NULL,
    ModBy              TEXT    NULL,
    MustChangePassword INTEGER NOT NULL DEFAULT 0
                               CHECK (MustChangePassword IN (0,1))
);

CREATE UNIQUE INDEX ux_users_username ON Users (UserName COLLATE NOCASE);

CREATE TABLE Settings (
    SettingKey   TEXT PRIMARY KEY,
    SettingValue TEXT NOT NULL,
    SettingType  TEXT NOT NULL DEFAULT 'string'
                 CHECK (SettingType IN ('string','integer','boolean','secret','url')),
    IsSecret     INTEGER NOT NULL DEFAULT 0
                 CHECK (IsSecret IN (0,1)),
    ModAt        TEXT NOT NULL,
    ModBy        TEXT NULL
);

CREATE TABLE Sessions (
    SessionID         TEXT PRIMARY KEY,
    TokenHash         TEXT NOT NULL UNIQUE,
    UserID            INTEGER NOT NULL
                      REFERENCES Users(UserID) ON DELETE CASCADE,
    CreatedAt         TEXT NOT NULL,
    LastAccessed      TEXT NOT NULL,
    ExpiresAt         TEXT NOT NULL,
    AbsoluteExpiresAt TEXT NOT NULL,
    RevokedAt         TEXT NULL,
    ClientIP          TEXT NULL,
    UserAgent         TEXT NULL
);

CREATE INDEX ix_sessions_user     ON Sessions (UserID);
CREATE INDEX ix_sessions_expiry   ON Sessions (ExpiresAt);
CREATE INDEX ix_sessions_absolute ON Sessions (AbsoluteExpiresAt);

INSERT INTO Settings (SettingKey, SettingValue, SettingType, IsSecret, ModAt, ModBy) VALUES
    ('GuildName',             'Aeternus',                                    'string',  0, '1970-01-01T00:00:00Z', 'system'),
    ('GuildOpenDays',         '14',                                          'integer', 0, '1970-01-01T00:00:00Z', 'system'),
    ('BazaarDays',            '30',                                          'integer', 0, '1970-01-01T00:00:00Z', 'system'),
    ('EQResourceBaseUrl',     'https://items.eqresource.com/items.php?id=',  'url',     0, '1970-01-01T00:00:00Z', 'system'),
    ('MariaDbHost',           '',                                            'string',  0, '1970-01-01T00:00:00Z', 'system'),
    ('MariaDbPort',           '3306',                                        'integer', 0, '1970-01-01T00:00:00Z', 'system'),
    ('MariaDbDatabase',       'GuildBank',                                   'string',  0, '1970-01-01T00:00:00Z', 'system'),
    ('MariaDbUsername',       '',                                            'string',  0, '1970-01-01T00:00:00Z', 'system'),
    ('MariaDbPassword',       '',                                            'secret',  1, '1970-01-01T00:00:00Z', 'system'),
    ('SetupComplete',         '0',                                           'boolean', 0, '1970-01-01T00:00:00Z', 'system'),
    ('SessionIdleMinutes',    '30',                                          'integer', 0, '1970-01-01T00:00:00Z', 'system'),
    ('SessionAbsoluteHours',  '8',                                           'integer', 0, '1970-01-01T00:00:00Z', 'system');