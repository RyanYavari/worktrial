package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"

	"github.com/go-chi/chi/v5"
)

// ErrTrackNotFound is returned by library methods when a track ID does not exist.
// Using a sentinel allows handlers to distinguish 404 from 500 via errors.Is
// without comparing error message strings.
var ErrTrackNotFound = errors.New("track not found")

// Track represents a single audio track in the content library.
// segment_path points at the HLS segments directory uploaded to Cloudflare R2.
type Track struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	SegmentPath string `json:"segment_path"`
	IsActive    bool   `json:"is_active"`
}

// ContentLibrary is the contract all handlers depend on.
// The interface allows the backing store to be swapped (e.g. PostgreSQL)
// without changing any handler code — only main.go wiring changes.
type ContentLibrary interface {
	GetTrack(id string) (Track, error)
	ListTracks() ([]Track, error)
	AddTrack(track Track) error
	DeactivateTrack(id string) error
}

// InMemoryLibrary is the demo implementation of ContentLibrary.
// Tracks are loaded from tracks.json at startup and held in a map.
// sync.RWMutex allows concurrent reads from admin API handlers.
type InMemoryLibrary struct {
	tracks map[string]Track
	mu     sync.RWMutex
}

// NewInMemoryLibrary reads the JSON file at path, unmarshals it into a slice
// of Tracks, and builds an in-memory map keyed by track ID.
// Called once at startup — failure means the service cannot manage content.
func NewInMemoryLibrary(path string) (*InMemoryLibrary, error) {
	// Read the entire file into memory. tracks.json is small — no streaming needed.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Unmarshal the JSON array into a slice, then index by ID for O(1) lookups.
	var tracks []Track
	if err := json.Unmarshal(data, &tracks); err != nil {
		return nil, err
	}

	m := make(map[string]Track, len(tracks))
	for _, t := range tracks {
		m[t.ID] = t
	}

	return &InMemoryLibrary{tracks: m}, nil
}

// GetTrack returns a single track by ID.
// RLock allows concurrent reads without blocking other readers.
func (l *InMemoryLibrary) GetTrack(id string) (Track, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	t, ok := l.tracks[id]
	if !ok {
		return Track{}, ErrTrackNotFound
	}
	return t, nil
}

// ListTracks returns all tracks regardless of is_active status.
// Map iteration order is non-deterministic — callers must not rely on ordering.
func (l *InMemoryLibrary) ListTracks() ([]Track, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	tracks := make([]Track, 0, len(l.tracks))
	for _, t := range l.tracks {
		tracks = append(tracks, t)
	}
	return tracks, nil
}

// AddTrack inserts or overwrites a track in the map keyed by track.ID.
func (l *InMemoryLibrary) AddTrack(track Track) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.tracks[track.ID] = track
	return nil
}

// DeactivateTrack sets is_active=false for the given track ID.
// This is a soft delete — the track stays in the map so existing references
// remain valid. Removing from the map would break any in-flight presigned URL
// that references the track's segment path.
func (l *InMemoryLibrary) DeactivateTrack(id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	t, ok := l.tracks[id]
	if !ok {
		return ErrTrackNotFound
	}

	// Map values are not addressable in Go — read, modify, write back.
	t.IsActive = false
	l.tracks[id] = t
	return nil
}

// LibraryHandler holds the ContentLibrary dependency for admin route handlers.
// Using the interface (not *InMemoryLibrary directly) keeps the handlers
// decoupled from the backing store — only main.go wiring changes if the
// store is swapped.
type LibraryHandler struct {
	Library ContentLibrary
}

// ListTracks handles GET /admin/tracks.
// Returns all tracks regardless of is_active — active filtering is the client's concern.
func (h *LibraryHandler) ListTracks(w http.ResponseWriter, r *http.Request) {
	tracks, err := h.Library.ListTracks()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// ListTracks always returns a slice (never nil), so the response is [] not null.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tracks)
}

// AddTrack handles POST /admin/tracks.
// Inserts or overwrites the track keyed by its ID field.
func (h *LibraryHandler) AddTrack(w http.ResponseWriter, r *http.Request) {
	// Decode the track from the request body.
	var track Track
	if err := json.NewDecoder(r.Body).Decode(&track); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if err := h.Library.AddTrack(track); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 201 Created — no body needed.
	w.WriteHeader(http.StatusCreated)
}

// DeactivateTrack handles DELETE /admin/tracks/{id}.
// Soft-deletes by setting is_active=false. The track stays in the library so
// in-flight presigned URLs that reference its segment path continue to resolve.
func (h *LibraryHandler) DeactivateTrack(w http.ResponseWriter, r *http.Request) {
	// Read the track ID from the Chi URL parameter.
	id := chi.URLParam(r, "id")

	err := h.Library.DeactivateTrack(id)
	if err != nil {
		// Distinguish "not found" from unexpected errors so clients receive a
		// meaningful status code rather than a generic 500.
		if errors.Is(err, ErrTrackNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 204 No Content — soft delete succeeded, nothing to return.
	w.WriteHeader(http.StatusNoContent)
}
