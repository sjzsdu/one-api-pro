package modelrouter

import (
	"sync"
	"time"
)

// ProfileStore is a local cache for model profiles with optional expiry.
type ProfileStore struct {
	mu       sync.RWMutex
	profiles map[string]*StoredProfile
	ttl      time.Duration
}

type StoredProfile struct {
	Profile   *ModelProfile
	ExpiresAt time.Time
}

func NewProfileStore(ttl time.Duration) *ProfileStore {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &ProfileStore{
		profiles: make(map[string]*StoredProfile),
		ttl:      ttl,
	}
}

func (s *ProfileStore) Get(model string) (*ModelProfile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stored, ok := s.profiles[model]
	if !ok {
		return nil, false
	}
	if !stored.ExpiresAt.IsZero() && time.Now().After(stored.ExpiresAt) {
		return nil, false
	}
	return stored.Profile, true
}

func (s *ProfileStore) Set(model string, profile *ModelProfile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles[model] = &StoredProfile{
		Profile:   profile,
		ExpiresAt: time.Now().Add(s.ttl),
	}
}

func (s *ProfileStore) SetBatch(profiles map[string]*ModelProfile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for m, p := range profiles {
		s.profiles[m] = &StoredProfile{
			Profile:   p,
			ExpiresAt: now.Add(s.ttl),
		}
	}
}

func (s *ProfileStore) PurgeExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	count := 0
	for m, stored := range s.profiles {
		if !stored.ExpiresAt.IsZero() && now.After(stored.ExpiresAt) {
			delete(s.profiles, m)
			count++
		}
	}
	return count
}
