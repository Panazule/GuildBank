package service

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Panazule/guildbank/internal/domain"
)

// Item-name lookup bounds. The resolver reads at most two rows to prove
// ambiguity and at most six to feed the officer's candidate picker; the
// repository enforces the same upper bound.
const (
	itemNameMaxRunes       = 128
	itemNameProbeLimit     = 2
	itemNameCandidateLimit = 6
	itemNameCacheEntries   = 2000
	itemNameCacheTTL       = 10 * time.Minute
)

// ItemNameStatus is the outcome of one name lookup.
type ItemNameStatus string

const (
	ItemNameStatusUnique    ItemNameStatus = "unique"
	ItemNameStatusAmbiguous ItemNameStatus = "ambiguous"
	ItemNameStatusNotFound  ItemNameStatus = "not_found"
)

// ItemNameResolution is the result of resolving an item name against the
// read-only Items source. Ambiguous results always carry at least two
// candidates and are never resolved silently.
type ItemNameResolution struct {
	Name            string
	Status          ItemNameStatus
	Match           domain.Item
	Candidates      []domain.Item
	CaseInsensitive bool
}

// ItemNameReader is the narrow read-only source contract for name lookups. It
// is implemented by the MariaDB item repository.
type ItemNameReader interface {
	FindItemsByName(ctx context.Context, name string, limit int) ([]domain.Item, error)
}

// ItemNameResolver resolves an entered item name to its metadata identity. It
// is the single authoritative implementation used by the inventory create path
// and by the officer-facing lookup endpoint, so the form hint and the stored
// value can never disagree.
//
// The source is externally owned and effectively static, so results (including
// ambiguity) are cached for a short TTL. A refreshed Items export is visible
// after the TTL expires or the process restarts.
type ItemNameResolver struct {
	items      ItemNameReader
	now        func() time.Time
	maxEntries int
	ttl        time.Duration

	mu    sync.Mutex
	cache map[string]itemNameCacheEntry
}

type itemNameCacheEntry struct {
	resolution ItemNameResolution
	expires    time.Time
}

// NewItemNameResolver builds the resolver. A nil reader makes Resolve report
// ErrUnavailable instead of guessing, which keeps a degraded start explicit.
func NewItemNameResolver(items ItemNameReader) *ItemNameResolver {
	return &ItemNameResolver{
		items:      items,
		now:        time.Now,
		maxEntries: itemNameCacheEntries,
		ttl:        itemNameCacheTTL,
		cache:      make(map[string]itemNameCacheEntry),
	}
}

// NormalizeItemName trims, collapses whitespace, drops control characters and
// truncates at a rune boundary to the documented name bound.
func NormalizeItemName(raw string) string {
	var b strings.Builder
	lastSpace := false
	for _, r := range raw {
		switch {
		case unicode.IsControl(r):
			if r == '\n' || r == '\r' || r == '\t' {
				if !lastSpace {
					b.WriteByte(' ')
					lastSpace = true
				}
			}
		case unicode.IsSpace(r):
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		default:
			b.WriteRune(r)
			lastSpace = false
		}
	}
	name := strings.TrimSpace(b.String())
	runes := []rune(name)
	if len(runes) > itemNameMaxRunes {
		name = strings.TrimSpace(string(runes[:itemNameMaxRunes]))
	}
	return name
}

// Resolve looks the name up. An empty name is NotFound rather than an error so
// the caller owns the "name or EQUID is required" message.
func (s *ItemNameResolver) Resolve(ctx context.Context, raw string) (ItemNameResolution, error) {
	name := NormalizeItemName(raw)
	if name == "" {
		return ItemNameResolution{Name: name, Status: ItemNameStatusNotFound}, nil
	}
	if s == nil || s.items == nil {
		return ItemNameResolution{}, domain.ErrUnavailable
	}
	if cached, ok := s.lookup(name); ok {
		cached.Name = name
		cached.CaseInsensitive = cached.Status == ItemNameStatusUnique && cached.Match.Name != name
		return cached, nil
	}

	rows, err := s.items.FindItemsByName(ctx, name, itemNameProbeLimit)
	if err != nil {
		return ItemNameResolution{}, err
	}
	resolution := ItemNameResolution{Name: name}
	switch len(rows) {
	case 0:
		resolution.Status = ItemNameStatusNotFound
	case 1:
		resolution.Status = ItemNameStatusUnique
		resolution.Match = rows[0]
		// The server matched a name whose stored casing differs from what the
		// officer typed, which means the source collation is case-insensitive.
		resolution.CaseInsensitive = rows[0].Name != name
	default:
		resolution.Status = ItemNameStatusAmbiguous
		candidates, err := s.items.FindItemsByName(ctx, name, itemNameCandidateLimit)
		if err != nil {
			return ItemNameResolution{}, err
		}
		resolution.Candidates = candidates
	}
	s.store(name, resolution)
	return resolution, nil
}

func (s *ItemNameResolver) lookup(name string) (ItemNameResolution, bool) {
	key := strings.ToLower(name)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[key]
	if !ok || now.After(entry.expires) {
		return ItemNameResolution{}, false
	}
	return entry.resolution, true
}

func (s *ItemNameResolver) store(name string, resolution ItemNameResolution) {
	key := strings.ToLower(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.maxEntries > 0 && len(s.cache) >= s.maxEntries {
		for evict := range s.cache {
			delete(s.cache, evict)
			break
		}
	}
	s.cache[key] = itemNameCacheEntry{resolution: resolution, expires: s.now().Add(s.ttl)}
}
