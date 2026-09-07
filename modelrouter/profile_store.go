package modelrouter

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// ProfileStore holds an immutable-by-reader snapshot. A later database or
// catalog refresher can replace the snapshot atomically through Replace.
type ProfileStore struct {
	mu       sync.RWMutex
	profiles map[string]ModelProfile
}

func NewProfileStore(profiles []ModelProfile) *ProfileStore {
	store := &ProfileStore{}
	store.Replace(profiles)
	return store
}

func (s *ProfileStore) Replace(profiles []ModelProfile) {
	next := make(map[string]ModelProfile, len(profiles))
	for _, profile := range profiles {
		name := strings.TrimSpace(profile.Model)
		if name == "" {
			continue
		}
		profile.Model = name
		profile.Quality = cloneFloatMap(profile.Quality)
		profile.Sources = append([]string(nil), profile.Sources...)
		next[name] = profile
	}
	s.mu.Lock()
	s.profiles = next
	s.mu.Unlock()
}

func (s *ProfileStore) Snapshot(names []string) map[string]ModelProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]ModelProfile, len(names))
	for _, name := range names {
		if profile, ok := s.profiles[name]; ok {
			profile.Quality = cloneFloatMap(profile.Quality)
			profile.Sources = append([]string(nil), profile.Sources...)
			result[name] = profile
		}
	}
	return result
}

func LoadProfileStore(path string) (*ProfileStore, error) {
	if strings.TrimSpace(path) == "" {
		return NewProfileStore(nil), nil
	}
	data, err := os.ReadFile(expandHomeDir(path))
	if err != nil {
		return nil, fmt.Errorf("read model router profiles: %w", err)
	}
	var profiles []ModelProfile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return nil, fmt.Errorf("decode model router profiles: %w", err)
	}
	return NewProfileStore(profiles), nil
}

var (
	profileProviderOnce sync.Once
	profileProvider     ModelProfileProvider
	profileProviderErr  error
)

func defaultModelProfileProvider() (ModelProfileProvider, error) {
	profileProviderOnce.Do(func() {
		profilePath := os.Getenv("MODEL_ROUTER_PROFILE_PATH")
		// Fall back to bundled model profiles if not configured
		if profilePath == "" {
			profilePath = "modelrouter/artifacts/model_profiles.json"
		}
		store, err := LoadProfileStore(profilePath)
		if err != nil {
			// Log warning but continue with empty store
			fmt.Printf("warning: could not load model profiles from %s: %v\n", profilePath, err)
			store = NewProfileStore(nil)
		}
		profileProvider = defaultProfileProvider{store: store}
	})
	return profileProvider, profileProviderErr
}
