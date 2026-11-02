package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	mu      sync.RWMutex
	path    string
	schemas []Schema
}

func OpenStore(path string) (*Store, error) {
	store := &Store{path: path, schemas: []Schema{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &store.schemas); err != nil {
		return nil, err
	}
	if store.schemas == nil {
		store.schemas = []Schema{}
	}
	return store, nil
}

func (s *Store) List() []Schema {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Schema{}, s.schemas...)
}

func (s *Store) Add(schema Schema) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.schemas = append(s.schemas, schema)
	if err := s.persistLocked(); err != nil {
		s.schemas = s.schemas[:len(s.schemas)-1]
		return err
	}
	return nil
}

func (s *Store) Delete(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, schema := range s.schemas {
		if schema.ID != id {
			continue
		}
		previous := s.schemas
		s.schemas = append(append([]Schema{}, s.schemas[:i]...), s.schemas[i+1:]...)
		if err := s.persistLocked(); err != nil {
			s.schemas = previous
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (s *Store) persistLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".schemas-*.json")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if err := json.NewEncoder(file).Encode(s.schemas); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, s.path)
}
