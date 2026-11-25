package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"sync"
	"time"
)

type Cache struct {
	resp     Response
	lastSeen time.Time
}

var (
	cache   = make(map[string]Cache)
	cacheMu sync.Mutex
	fileMu  sync.Mutex
)

type Response struct {
	Names  map[string]string   `json:"names"`
	Relays map[string][]string `json:"relays"`
}

func nip05Path() string {
	return path.Join(config.WorkingDirectory, "/nip05.json")
}

func NIP05Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "Missing query parameter 'name'", http.StatusBadRequest)
		return
	}

	cacheMu.Lock()
	c, exist := cache[name]
	if exist {
		c.lastSeen = time.Now()
		cache[name] = c
	}
	cacheMu.Unlock()

	if exist {
		_ = json.NewEncoder(w).Encode(c.resp)
		return
	}

	doc, err := loadNIP05()
	if err != nil {
		http.Error(w, "Something went wrong", http.StatusInternalServerError)
		return
	}

	pubKey, ok := doc.Names[name]
	if !ok {
		http.Error(w, "Can't find this name", http.StatusNotFound)
		return
	}

	resp := Response{
		Names:  map[string]string{name: pubKey},
		Relays: make(map[string][]string, 1),
	}
	if relays, ok := doc.Relays[pubKey]; ok {
		resp.Relays[pubKey] = relays
	}

	cacheMu.Lock()
	cache[name] = Cache{lastSeen: time.Now(), resp: resp}
	cacheMu.Unlock()

	_ = json.NewEncoder(w).Encode(resp)
}

// loadNIP05 reads nip05.json; a missing file is treated as an empty document.
func loadNIP05() (*Response, error) {
	resp := &Response{}

	data, err := ReadFile(nip05Path())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, resp); err != nil {
			return nil, err
		}
	}

	if resp.Names == nil {
		resp.Names = make(map[string]string)
	}
	if resp.Relays == nil {
		resp.Relays = make(map[string][]string)
	}

	return resp, nil
}

func saveNIP05(resp *Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return WriteFile(nip05Path(), data)
}

func dropCache(name string) {
	cacheMu.Lock()
	delete(cache, name)
	cacheMu.Unlock()
}

func setNIP05(pubkey, name string) error {
	fileMu.Lock()
	defer fileMu.Unlock()

	resp, err := loadNIP05()
	if err != nil {
		return err
	}

	resp.Names[name] = pubkey
	if err := saveNIP05(resp); err != nil {
		return err
	}

	dropCache(name)
	return nil
}

func unSetNIP05(name string) error {
	fileMu.Lock()
	defer fileMu.Unlock()

	resp, err := loadNIP05()
	if err != nil {
		return err
	}

	delete(resp.Names, name)
	if err := saveNIP05(resp); err != nil {
		return err
	}

	dropCache(name)
	return nil
}

func checkCache() {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		cacheMu.Lock()
		for name, c := range cache {
			if time.Since(c.lastSeen) >= time.Hour {
				delete(cache, name)
			}
		}
		cacheMu.Unlock()
	}
}
