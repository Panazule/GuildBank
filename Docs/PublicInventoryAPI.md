# Public Inventory API v1

**Status:** Implemented in Phase 4 (2026-11-22 UTC)
**Base:** `/api/v1`
**Authentication:** None for the four public routes. An invalid cookie is ignored; an authenticated administrator receives the same item projection.

## Routes

| Method | Path | Response |
|---|---|---|
| GET | `/api/v1/items` | Paginated public inventory collection |
| GET | `/api/v1/items/{id}` | One active public inventory record |
| GET | `/` | HTML inventory list |
| GET | `/inventory/{id}` | HTML inventory detail |

`POST`, `PUT` and `DELETE` on the API item routes return `405`. There is no
public write route.

## Collection query parameters

| Parameter | Default | Rules |
|---|---|---|
| `q` | empty | Trimmed; at most 128 Unicode code points; case-insensitive substring of `Name`; `%`, `_`, `!` are literal. |
| `class` | empty | One of `WAR, CLR, PAL, RNG, SHD, DRU, MNK, BRD, ROG, SHM, NEC, WIZ, MAG, ENC, BST, BER` or `ALL`. Case-insensitive; empty/`ALL` means no predicate. |
| `race` | empty | One canonical race label or `ALL`. Case-insensitive; space-containing labels such as `Wood Elf` are accepted. |
| `itemType` | empty | `Item`, `Spell` or `all`; case-insensitive. |
| `sort` | `name` | `name`, `name_desc`, `date_added`, `date_added_desc`, `date_open`, `date_open_desc`, `date_bazaar`, `date_bazaar_desc`. |
| `page` | `1` | Integer `1..10000`. |
| `pageSize` | `50` | Integer `1..100`; maximum 100. |

All predicates are combined with AND. A specific class/race matches stored
`ALL` or a comma-boundary membership token; blank restrictions never match a
specific class/race. `DeletedFg=0` is unconditional.

## Collection response

```json
{
  "data": {
    "items": [
      {
        "id": 186,
        "equid": 8498,
        "name": "Ancient Burrower Hide Tunic",
        "itemType": "Item",
        "quantity": 1,
        "classString": "CLR,PAL,RNG,SHD,DRU,BRD,ROG,SHM,BST,BER",
        "raceString": "ALL",
        "metadataMissing": false,
        "dateAdded": "2026-11-22",
        "dateOpen": "2026-12-06",
        "dateBazaar": "2026-12-22",
        "availability": "upcoming",
        "eqResourceUrl": "https://items.eqresource.com/items.php?id=8498"
      }
    ]
  },
  "meta": {
    "page": 1,
    "pageSize": 50,
    "total": 706,
    "totalPages": 15,
    "sort": "name"
  }
}
```

`items` is always an array. `totalPages` is `0` when `total` is `0`. A page
beyond the result set returns `200`, an empty array and the requested page in
meta. Count and rows come from one repeatable-read snapshot. When page 1 has no
rows, the snapshot is known empty, so the exact count is not repeated.

## Detail response

```json
{
  "data": {
    "item": {
      "id": 186,
      "equid": 8498,
      "name": "Ancient Burrower Hide Tunic",
      "itemType": "Item",
      "quantity": 1,
      "classString": "CLR,PAL,RNG,SHD,DRU,BRD,ROG,SHM,BST,BER",
      "raceString": "ALL",
      "metadataMissing": false,
      "dateAdded": "2026-11-22",
      "dateOpen": "2026-12-06",
      "dateBazaar": "2026-12-22",
      "availability": "upcoming",
      "eqResourceUrl": "https://items.eqresource.com/items.php?id=8498"
    }
  }
}
```

Absent, non-positive, malformed or soft-deleted IDs return `404 not_found`.

## Availability

One UTC calendar date is captured per request. `DateOpen` and `DateBazaar` are
inclusive:

| State | Condition |
|---|---|
| `upcoming` | `today < DateOpen` |
| `open` | `DateOpen <= today <= DateBazaar` |
| `bazaar` | `today > DateBazaar` and `today >= DateOpen` |

An inverted stored window (`DateBazaar < DateOpen`) is never presented as open:
it remains upcoming until its open date, then becomes bazaar. Phase 5 is
responsible for validating `DateOpen <= DateBazaar` on new writes.

## EQResource URL

`eqResourceUrl` is built only from a stored setting that revalidates as exactly
HTTPS `items.eqresource.com/items.php?id=`, with a positive decimal EQUID
appended. No client value is accepted, no network request is made, and an
invalid/missing setting yields `null`. HTML links use
`target="_blank" rel="noopener noreferrer"`.

## Errors, headers and privacy

Errors use the existing envelope:

```json
{"error":{"code":"validation_failed","message":"one or more fields are invalid","fields":{"sort":"sort must be one of the approved values"},"request_id":"..."}}
```

| Status | Code |
|---|---|
| `422` | `validation_failed`, with a field keyed by parameter name |
| `404` | `not_found` |
| `503` | `dependency_unavailable` (store outage or five-second request deadline) |
| `405` | `method_not_allowed` |
| `500` | `internal_error` without SQL, paths or settings |

Public responses carry the same security headers as the rest of the
application, including `Cache-Control: no-store` and the strict CSP. They never
include `SourceLocation`, `GivenTo`, `DeletedFg`, `ModAt`, `ModBy`,
`ModByUserID`, `NameSort`, import warnings or audit data. Authentication does
not change the item payload; a future administrator DTO is Phase 5 work.