// Package fingerprint computes the canonical content hash of a Skill folder.
// The fingerprint is the version identity recorded at install time and compared
// against the Source's current fingerprint to detect Update available (upstream
// drift). See ADR 0001.
package fingerprint

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Dir returns a deterministic SHA-256 fingerprint over the contents of dir. The
// hash covers each regular file's path (relative to dir, slash-normalised) and
// its bytes, in a stable sorted order — so it is independent of filesystem walk
// order and changes when any file is added, removed, renamed, or edited.
func Dir(dir string) (string, error) {
	if info, err := os.Stat(dir); err != nil {
		return "", err
	} else if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}

	type entry struct {
		rel  string
		full string
	}
	var files []entry
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil // directories are implied by paths; skip symlinks/specials
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, entry{rel: filepath.ToSlash(rel), full: path})
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })

	h := sha256.New()
	for _, f := range files {
		// Length-prefix the path so distinct (path, content) splits cannot
		// collide, then mix in the file bytes.
		writeField(h, f.rel)
		data, err := os.ReadFile(f.full)
		if err != nil {
			return "", err
		}
		writeLen(h, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeField(h interface{ Write([]byte) (int, error) }, s string) {
	writeLen(h, len(s))
	_, _ = h.Write([]byte(s))
}

func writeLen(h interface{ Write([]byte) (int, error) }, n int) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(n))
	_, _ = h.Write(buf[:])
}
