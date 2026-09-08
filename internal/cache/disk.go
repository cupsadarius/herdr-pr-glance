// Package cache persists complete discussions beneath Herdr's state directory.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

const schemaVersion = 1
const retention = 7 * 24 * time.Hour

type Disk struct{ dir string }

func New(stateDir string) *Disk { return &Disk{dir: filepath.Join(stateDir, "cache")} }
func canonical(pr model.PR) string {
	return fmt.Sprintf("https://%s/%s/pull/%d", strings.ToLower(pr.Host), strings.ToLower(strings.Trim(pr.Repository, "/")), pr.Number)
}
func (c *Disk) path(pr model.PR, section model.Section) string {
	sum := sha256.Sum256([]byte(canonical(pr) + "\n" + string(section)))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:])+".json")
}
func validSection(section model.Section) bool {
	return section == model.Comments || section == model.Reviews
}
func (c *Disk) prepare(now time.Time) error {
	if err := os.MkdirAll(c.dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(c.dir, 0700); err != nil {
		return err
	}
	files, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		path := filepath.Join(c.dir, f.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var e model.CacheEntry
		if json.Unmarshal(raw, &e) == nil && !e.FetchedAt.IsZero() && now.Sub(e.FetchedAt) > retention {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

// Get returns stale entries too. Read/corruption errors are misses with an error
// so callers can fetch on demand and display a warning if appropriate.
func (c *Disk) Get(pr model.PR, section model.Section, now time.Time) (model.CacheEntry, bool, error) {
	var e model.CacheEntry
	if !validSection(section) {
		return e, false, fmt.Errorf("invalid cache section %q", section)
	}
	if err := c.prepare(now); err != nil {
		return e, false, err
	}
	raw, err := os.ReadFile(c.path(pr, section))
	if errors.Is(err, os.ErrNotExist) {
		return e, false, nil
	}
	if err != nil {
		return e, false, err
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		return model.CacheEntry{}, false, err
	}
	if e.SchemaVersion != schemaVersion || canonical(e.PR) != canonical(pr) || e.Section != section || !e.Data.Complete || e.Data.Section != section || canonical(e.Data.PR) != canonical(pr) || e.FetchedAt.IsZero() {
		return model.CacheEntry{}, false, errors.New("incompatible or incomplete cache entry")
	}
	return e, true, nil
}

// Put never mutates data. An error leaves the fetched payload usable in memory
// and the previous complete entry intact, including on a failed rename.
func (c *Disk) Put(pr model.PR, section model.Section, data model.Discussion, now time.Time) error {
	if !validSection(section) || !data.Complete || data.Section != section || canonical(data.PR) != canonical(pr) {
		return errors.New("only complete matching discussions can be cached")
	}
	if err := c.prepare(now); err != nil {
		return err
	}
	data.FetchedAt = now
	raw, err := json.Marshal(model.CacheEntry{SchemaVersion: schemaVersion, PR: pr, Section: section, FetchedAt: now, Data: data})
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(c.dir, ".discussion-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(raw); err != nil {
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
	return os.Rename(name, c.path(pr, section))
}
