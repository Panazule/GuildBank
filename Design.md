Aeternus Guild Bank
Design.md v1.6.1
Authoritative Project Specification
Project Overview
Aeternus Guild Bank is a self-hosted EverQuest guild inventory management application designed to run as a Docker container on Unraid.

The application provides:

Public inventory browsing
Administrative inventory management
EverQuest inventory import processing
Audit tracking
User administration
Configurable inventory lifecycle management
Future integration support through REST APIs
The primary goal is to provide guild members with visibility into guild inventory while allowing guild officers to efficiently manage acquisitions, availability windows, and eventual bazaar disposition.

Core Design Goals
Public Users
Public users shall be able to:

View guild inventory
Search inventory
Sort inventory
Filter inventory
View class restrictions
View race restrictions
View item availability dates
Open item information on EQResource
Authentication shall not be required.

Administrative Users
Administrators shall be able to:

Login securely
Import inventory exports
Add inventory manually
Update inventory
Remove inventory
Manage configuration
Manage users
Review audit history
Administrative functionality shall only be visible to authenticated users.

Technology Stack
Backend
Language:

Go

Recommended Framework:

Gin

Frontend
Server-side rendered HTML.

Recommended technologies:

HTMX
Bootstrap 5
Deployment
Docker

Target Platform:

Unraid

Database Architecture
The application intentionally separates:

Application Data

from

Guild Data

SQLite
Purpose:

Application configuration and authentication.

Location:

/config/guildbank.db

Tables:

Users
Settings
Sessions
MariaDB
Database Name:

GuildBank

Purpose:

Operational guild data.

Tables:

Items
GuildBankItems
ImportBatch
ImportList
Audit
Application Architecture
The application shall use a layered architecture.

UI Layer ↓ REST API Layer ↓ Service Layer ↓ Repository Layer ↓ Database Layer

Architectural Rules
The UI shall never modify database tables directly.

All create, update, and delete operations must pass through REST endpoints.

All REST endpoints must pass through business logic services.

All database access must occur through repositories.

Benefits:

Validation consistency
Security consistency
Audit consistency
Future integrations
Easier testing
Easier maintenance
First Run Experience
If SQLite database does not exist:

/config/guildbank.db

the application enters Setup Mode.

Setup Wizard
Step 1
Create Administrator

Fields:

Username
Password
Confirm Password
Step 2
Configure MariaDB

Fields:

Host
Port
Database
Username
Password
Step 3
Validate Connection

Successful connection required.

Step 4
Store Configuration

Configuration stored in SQLite.

SQLite Tables
Users
Purpose:

Administrative authentication.

Fields:

UserID (PK)
UserName
PasswordHash
AccessLevel
LastLogin
DisabledFg
ModAt
Passwords shall be stored using bcrypt.

Passwords shall never be stored in plaintext.

Initial AccessLevel:

Admin

Settings
Purpose:

Application configuration.

Fields:

SettingKey
SettingValue
Initial settings:

GuildName
GuildOpenDays
BazaarDays
EQResourceBaseUrl
Default values:

GuildName = Aeternus

GuildOpenDays = 14

BazaarDays = 30

EQResourceBaseUrl = https://items.eqresource.com/items.php?id=

All settings shall be editable through the administration UI.

Sessions
Purpose:

Authentication sessions.

Fields:

SessionID
UserID
CreatedAt
LastAccessed
ExpiresAt
MariaDB Database
Database Name:

GuildBank

Table: Items
Purpose:

Imported EverQuest Item Collector database.

Source:

EQ Item Collector Export

The application shall treat this table as read-only.

The application shall not update this table.

Future versions may support import automation.

Items Contains
Examples:

EQUID
Name
ClassList
RaceList
Stats
Effects
Requirements
Restrictions
and all imported collector fields.

Table: GuildBankItems
Purpose:

Tracks current and historical guild inventory.

Fields
GuildBankItemID
Primary Key

ItemType
Values:

Item
Spell
EQUID
References:

Items.ID

Name
Copied from Items table.

Stored locally for searching and sorting.

ClassString
Decoded class bitmask.

Examples:

WAR,CLR

WAR,PAL,RNG,SHD,BRD

ALL

Stored permanently.

RaceString
Decoded race bitmask.

Examples:

Human,Erudite

Human,Barbarian

ALL

Stored permanently.

DateAdded
Date entered guild inventory.

DateOpen
Calculated during creation.

Formula:

DateAdded + GuildOpenDays

Stored permanently.

DateBazaar
Calculated during creation.

Formula:

DateAdded + BazaarDays

Stored permanently.

Quantity
Rules:

Items:

Always 1

Spells:

May exceed 1

SourceLocation
Original source location.

Examples:

General 5-Slot1
Bank4-Slot2
Hoard47
GivenTo
Optional text field.

Examples:

Marc
Icefear
Alt Bank
DeletedFg
Soft delete flag.

Values:

0 = Active

1 = Removed

Records shall never be physically deleted.

ModAt
Modification timestamp.

ModBy
Administrative user.

Inventory Classification Rules
Classification shall be automatic.

No administrator input required.

Spell Records
Names beginning with:

Spell:
Song:
Ancient:
shall become:

ItemType = Spell

Behavior:

Single Row
Quantity Maintained
Example:

Spell: Torpor

Quantity = 12

Item Records
All other inventory shall be classified as:

Item

Behavior:

One Row Per Item Instance

Example:

Quantity = 5

Creates:

5 GuildBankItems records

Class Bitmask Decoding
ClassList contains bitmask values.

GuildBankItems stores decoded strings.

WAR = 1

CLR = 2

PAL = 4

RNG = 8

SHD = 16

DRU = 32

MNK = 64

BRD = 128

ROG = 256

SHM = 512

NEC = 1024

WIZ = 2048

MAG = 4096

ENC = 8192

BST = 16384

BER = 32768

Special Rule:

65535 = ALL

Example:

3

WAR,CLR

Race Bitmask Decoding
RaceList contains bitmask values.

GuildBankItems stores decoded strings.

Human = 1

Barbarian = 2

Erudite = 4

Wood Elf = 8

High Elf = 16

Dark Elf = 32

Half Elf = 64

Dwarf = 128

Troll = 256

Ogre = 512

Halfling = 1024

Gnome = 2048

Iksar = 4096

Vah Shir = 8192

Froglok = 16384

Drakkin = 32768

Special Rule:

65535 = ALL

Example:

5

Human,Erudite

Validation Test
Item:

Blade of Carnage

Expected:

ClassString

WAR,PAL,RNG,SHD,BRD

Expected:

RaceString

ALL

This shall be used to validate bitmask decoding logic.

Table: ImportBatch
Purpose:

Tracks inventory uploads.

Fields:

ImportBatchID
FileName
ImportedBy
ImportedAt
Status
Status values:

Uploaded
Processing
Completed
Failed
Import history shall be retained.

Table: ImportList
Purpose:

Inventory staging.

Import data shall remain available for review and troubleshooting.

Fields:

ImportID
ImportBatchID
EQUID
Name
Quantity
LocationRaw
LocationGroup
SelectedFg
Location Processing
Original:

General 5-Slot1

General 5-Slot6

General 5-Slot8

Normalized:

General 5

Original:

Bank4-Slot1

Bank4-Slot6

Normalized:

Bank 4

Original:

Hoard1

Hoard2

Hoard7

Hoard245

Normalized:

Hoard

for filtering and selection purposes.

Import Workflow
Step 1
Upload inventory export.

Step 2
Create ImportBatch.

Step 3
Parse file.

Step 4
Discard records where:

ID = 0

or

Name = Empty

Step 5
Generate LocationGroup values.

Step 6
Populate ImportList.

Step 7
Build Location Selector

Distinct LocationGroups become selectable.

Examples:

General 1
General 2
General 3
Bank 1
Bank 2
SharedBank 1
Hoard
Step 8
Administrator selects desired locations.

Grid updates dynamically.

Step 9
Inventory Grid

All records default to selected.

Example:

☑ Blade of Carnage

☑ Fungi Covered Scale Tunic

☑ Spell: Torpor

☐ Rusty Dagger

Administrator may deselect individual records.

Step 10
Import Selected

Selected records committed to GuildBankItems.

Import Summary
Before commit display:

Locations Selected
Items Selected
Spells Selected
Total Records To Import
Commit Logic
Items
Example:

Quantity = 5

Creates:

5 GuildBankItems records

Spells
If spell already exists:

Increase Quantity

If spell does not exist:

Create Record

Sorting Requirements
Natural sorting must be used.

Incorrect:

General 1

General 10

General 11

General 2

Correct:

General 1

General 2

General 3

General 10

General 11

Applies to:

Location selectors
Import views
Inventory views
REST API Requirements
All business operations shall be exposed through REST endpoints.

The web UI shall consume these endpoints.

Future external tools may consume these endpoints.

Inventory Endpoints
GET /api/items

GET /api/items/{id}

POST /api/items

PUT /api/items/{id}

DELETE /api/items/{id}

Import Endpoints
POST /api/imports

GET /api/imports

GET /api/imports/{id}

POST /api/imports/{id}/commit

User Endpoints
GET /api/users

POST /api/users

PUT /api/users/{id}

DELETE /api/users/{id}

Settings Endpoints
GET /api/settings

PUT /api/settings

Audit Endpoints
GET /api/audit

Audit Requirements
All REST write operations shall create audit entries.

Including:

Item Creation
Item Updates
Item Removal
Imports
User Changes
Settings Changes
EQResource Links
Generated dynamically.

Example:

EQUID = 28813

Produces:

https://items.eqresource.com/items.php?id=28813

Opens in a new browser tab.

Security Requirements
bcrypt password hashing
secure session management
secure cookies
CSRF protection
session expiration
login rate limiting
administrator-only secured routes
Administrative Screens
Dashboard
Inventory
Imports
Users
Settings
Audit
Public Screens
Guild Inventory

Features:

Search
Filtering
Sorting
Natural Sorting
Pagination
Class Filtering
Race Filtering
EQResource Links
No authentication required.

Future Enhancements
Not required for Version 1.

Potential additions:

Discord Integration
Item Requests
Reservation Workflows
Role Based Security
Inventory Exports
Multi Guild Support
Automated Item Imports
Design Philosophy
Aeternus Guild Bank prioritizes:

Simplicity
Maintainability
Performance
Ease of Operation
The Items table serves as the authoritative source of EverQuest item metadata.

GuildBankItems intentionally stores decoded ClassString and RaceString values to avoid repeated bitmask processing during normal application operation.

Authentication and configuration are intentionally isolated within SQLite while guild inventory, imports, and auditing are maintained within MariaDB.

All modifications flow through REST APIs to guarantee consistent validation, security, auditing, and future extensibility.


Version 1.6 Final Decisions
This section supersedes any conflicting statements elsewhere within this document.

D-01 Canonical Item Identifier
Items.ID is the authoritative EverQuest Item identifier.

Throughout the application this value may also be referred to as EQUID.

The following values represent the same identifier:

Items.ID
GuildBankItems.EQUID
ImportList.EQUID
Application-generated identifiers remain separate.

Examples:

GuildBankItemID
ImportID
ImportBatchID
These identifiers are unrelated to EQUID values.

D-02 Setup Mode
Setup Mode shall only exist when no administrator account exists within the SQLite Users table.

Startup logic:

Open SQLite database.
Verify Users table exists.
Check for administrator accounts.
If no administrator accounts exist:

Enter Setup Mode.
If one or more administrator accounts exist:

Setup Mode is disabled.
The existence or absence of the SQLite database file itself shall not determine Setup Mode availability.

D-03 HTMX / REST Architecture
HTMX may be used for user interface interactions.

All HTMX requests shall invoke REST endpoints.

REST endpoints shall invoke Service Layer methods.

Service Layer methods shall invoke Repository Layer methods.

Repository Layer methods shall perform database operations.

Direct database access from the UI layer is prohibited.

Required request flow:

UI

↓

HTMX

↓

REST Endpoint

↓

Service Layer

↓

Repository Layer

↓

Database

D-04 Import Format Contract
The repository contains the canonical import example:

Docs/Samples/InventoryExport-Sample.txt

This file serves as the authoritative import-format reference.

Parser implementations shall be validated against this sample file.

The sample file includes:

Equipment
Spells
Ancient spells
General inventory locations
Bank locations
Shared Bank locations
Hoard locations
Changes to import processing must remain compatible with this file unless intentionally revised.

D-05 Location Normalization
Location values shall be normalized for inventory filtering and selection.

Rules:

General-Slot

→ General

Bank-Slot

→ Bank

SharedBank-Slot

→ SharedBank

Hoard

→ Hoard

Examples:

General5-Slot1

→ General 5

General5-Slot8

→ General 5

Bank4-Slot2

→ Bank 4

SharedBank2-Slot6

→ SharedBank 2

Hoard17

→ Hoard

Hoard145

→ Hoard

LocationRaw preserves the original imported value.

LocationGroup stores the normalized value.

LocationGroup shall be used for filtering, grouping and selection.

D-06 Spell Merge Rules
Spell identity is determined by:

ItemType = Spell
EQUID
When importing a spell:

If a matching spell already exists:

New Quantity = Existing Quantity + Imported Quantity

Example:

Existing Quantity = 5

Imported Quantity = 3

Result Quantity = 8

DateAdded, DateOpen and DateBazaar shall remain unchanged when spell quantities are increased.

If no matching spell exists:

Create a new GuildBankItems record.
D-07 Item Import Rules
Items are never merged.

Each imported item instance creates its own GuildBankItems record.

Example:

Imported Quantity = 5

Result:

5 independent GuildBankItems records.

Each record receives:

Unique GuildBankItemID
DateAdded
DateOpen
DateBazaar
SourceLocation
This supports independent lifecycle tracking.

D-08 Missing Metadata Behavior
If an imported EQUID cannot be found in the Items table:

The record shall still be imported.

The original EQUID shall be preserved.

ClassString shall be blank.

RaceString shall be blank.

MetadataMissingFg shall be set to True.

An import warning shall be created.

The item remains visible in inventory and may be corrected later after the Items table is updated.

D-09 Session Security
SQLite Sessions table shall contain:

SessionID
UserID
TokenHash
CreatedAt
LastAccessed
ExpiresAt
Session tokens shall never be stored in plaintext.

Only cryptographic token hashes shall be persisted.

The browser receives the raw session token.

SQLite stores only the hash equivalent.

D-10 Audit Table Definition
MariaDB shall contain an Audit table.

Audit records are append-only.

Audit records shall never be modified or deleted by the application.

Fields:

AuditID
EventDate
UserID
UserName
EventType
TableName
RecordID
OldValues
NewValues
Notes
OldValues and NewValues shall be stored as JSON when applicable.

Example EventTypes:

LOGIN
LOGOUT
IMPORT_CREATED
IMPORT_COMMITTED
ITEM_CREATED
ITEM_UPDATED
ITEM_REMOVED
USER_CREATED
USER_UPDATED
USER_DISABLED
SETTINGS_UPDATED
D-11 Import Selection Endpoint
The following endpoint shall exist:

PUT /api/imports/{batchId}/selection

Purpose:

Persist ImportList.SelectedFg values.

The request payload shall contain the selected ImportID values.

This endpoint supports the staged review and deselection workflow during imports.

D-12 Additional Security Requirements
All authenticated write operations shall require CSRF validation.

Session cookies shall be HttpOnly.

Session cookies shall be Secure when HTTPS is enabled.

Login attempts shall be rate limited.

Recommended defaults:

5 failed attempts
15-minute lockout
All dynamic sorting and filtering must use allowlisted column mappings or parameterized query generation.

Client-supplied values shall never be concatenated directly into SQL.

D-13 SQLite / MariaDB Separation
SQLite stores:

Users
Settings
Sessions
MariaDB connection information
MariaDB stores:

Items
GuildBankItems
ImportBatch
ImportList
Audit
This separation is intentional and shall be maintained.

Version 1.6.1 Amendments
This section supersedes any conflicting statements elsewhere within this document, including the Version 1.6 Final Decisions.

Version 1.6.1 incorporates five approved corrections. No other behavior changes.

AM-01 Unresolved Item Identity
The original imported EQUID shall be preserved.

A sentinel identifier, including EQUID = -1, shall not be used.

GuildBankItems.EQUID shall continue to represent the EverQuest item identifier and shall not be rewritten.

An unresolved identifier is by definition absent from Items at import time, so the physical foreign key from GuildBankItems.EQUID to Items.ID shall not be enforced.

Referential validity shall be validated in the service layer whenever the referenced Items row is available.

Missing metadata shall be recorded with MetadataMissingFg rather than by altering the identifier.

AM-02 MetadataMissingFg
MetadataMissingFg is a GuildBankItems field.

Values:

False

True

Default: False

MetadataMissingFg shall be set to True when the imported EQUID is absent from Items at commit time.

While MetadataMissingFg is True:

ClassString shall be blank.

RaceString shall be blank.

The record shall remain visible in guild inventory.

An import warning shall be created.

When Items metadata becomes available, the record may be refreshed; ClassString and RaceString are repopulated and MetadataMissingFg is set to False.

This adds MetadataMissingFg to the GuildBankItems field list.

AM-03 Import Sample Path
The canonical import example is:

Docs/Samples/InventoryExport-Sample.txt

This corrects the path declared by D-04.

The file is tab-delimited and its header row is:

Location Name ID Count Slots

Column handling:

Location → LocationRaw

Name → Name

ID → EQUID

Count → Quantity

Slots → collector metadata, not stored in guild inventory

Rows may omit trailing columns.

A row with three fields supplies Location, Name and ID; Count defaults to 1 and Slots is empty.

A row with four fields supplies Count when the fourth field is numeric; otherwise the fourth field is Slots and Count defaults to 1.

Blank lines shall be skipped.

Section header rows shall be skipped; a section header row has a non-numeric ID and a Name field equal to Name.

The file shall be read as UTF-8.

Rows with ID = 0 or an empty Name shall be discarded.

Parser implementations shall be validated against this file.

AM-04 Hoard Terminology
The canonical location group is Hoard.

The import sample, D-05, the location selector and all user-facing text shall use Hoard.

No other spelling of this location group shall be used.

AM-05 Version Metadata
The document title shall read Design.md v1.6.1.

References to earlier document versions are superseded by this section.

Change log:

1.6 - Version 1.6 Final Decisions (D-01 through D-13) added.

1.6.1 - Original EQUID preserved; MetadataMissingFg added; sample path corrected; Hoard terminology standardized; version metadata updated.