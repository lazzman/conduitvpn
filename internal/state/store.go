// Package state persists app data as JSON files with atomic writes
// (write-temp-then-rename) so a crash never leaves a half-written file.
package state

import (
	"encoding/json"
	"os"
	"path/filepath"

	"aimilivpn/internal/node"
)

type Store struct {
	dir string
}

func NewStore(dir string) *Store { return &Store{dir: dir} }

func (s *Store) NodesPath() string { return filepath.Join(s.dir, "nodes.json") }

func (s *Store) SaveNodes(nodes []*node.Node) error {
	return writeJSON(s.NodesPath(), nodes)
}

func (s *Store) LoadNodes() ([]*node.Node, error) {
	var nodes []*node.Node
	if err := readJSON(s.NodesPath(), &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
