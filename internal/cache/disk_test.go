package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

func fixture() (model.PR, model.Discussion, time.Time) {
	p := model.PR{Host: "github.com", Repository: "owner/repo", Number: 7, URL: "https://github.com/owner/repo/pull/7"}
	return p, model.Discussion{PR: p, Section: model.Comments, Complete: true, Comments: []model.Comment{{ID: "old"}}}, time.Unix(1000, 0)
}
func TestDiskFreshnessAndCompleteReplacement(t *testing.T) {
	p, d, now := fixture()
	c := New(t.TempDir())
	if err := c.Put(p, model.Comments, d, now); err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int{299, 300} {
		e, ok, err := c.Get(p, model.Comments, now.Add(time.Duration(offset)*time.Second))
		if err != nil || !ok || e.Fresh(now.Add(time.Duration(offset)*time.Second)) != (offset == 299) {
			t.Fatalf("%+v %v %v", e, ok, err)
		}
	}
	d.Complete = false
	if err := c.Put(p, model.Comments, d, now); err == nil {
		t.Fatal("accepted partial")
	}
	e, ok, err := c.Get(p, model.Comments, now)
	if err != nil || !ok || e.Data.Comments[0].ID != "old" {
		t.Fatal(e, ok, err)
	}
}
func TestDiskKeysPermissionsCorruptionCleanup(t *testing.T) {
	root := t.TempDir()
	c := New(root)
	p, d, now := fixture()
	for _, q := range []model.PR{p, {Host: "ghe.example", Repository: p.Repository, Number: 7}, {Host: p.Host, Repository: "other/repo", Number: 7}} {
		d.PR = q
		if err := c.Put(q, model.Comments, d, now); err != nil {
			t.Fatal(err)
		}
	}
	d.PR = p
	d.Section = model.Reviews
	if err := c.Put(p, model.Reviews, d, now); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(filepath.Join(root, "cache"))
	if len(files) != 4 {
		t.Fatal(len(files))
	}
	info, _ := os.Stat(filepath.Join(root, "cache"))
	if info.Mode().Perm() != 0700 {
		t.Fatal(info.Mode())
	}
	for _, f := range files {
		info, _ := f.Info()
		if info.Mode().Perm() != 0600 {
			t.Fatal(info.Mode())
		}
	}
	path := c.path(p, model.Comments)
	for _, bad := range []string{"broken", `{"SchemaVersion":999}`} {
		os.WriteFile(path, []byte(bad), 0600)
		_, ok, err := c.Get(p, model.Comments, now)
		if ok || err == nil {
			t.Fatal(ok, err)
		}
	}
	if _, _, err := c.Get(p, model.Reviews, now.Add(7*24*time.Hour+time.Second)); err != nil {
		t.Fatal(err)
	}
	files, _ = os.ReadDir(filepath.Join(root, "cache"))
	for _, f := range files {
		if f.Name() != filepath.Base(path) {
			t.Fatal("old entry retained", f.Name())
		}
	}
}
func TestDiskWriteFailureAndAtomicReplacement(t *testing.T) {
	root := t.TempDir()
	c := New(root)
	p, d, now := fixture()
	if err := c.Put(p, model.Comments, d, now); err != nil {
		t.Fatal(err)
	}
	d.Comments[0].ID = "new"
	if err := c.Put(p, model.Comments, d, now); err != nil {
		t.Fatal(err)
	}
	e, ok, err := c.Get(p, model.Comments, now)
	if err != nil || !ok || e.Data.Comments[0].ID != "new" {
		t.Fatal(e, ok, err)
	}
	os.Remove(c.path(p, model.Comments))
	os.Mkdir(c.path(p, model.Comments), 0700)
	if err := c.Put(p, model.Comments, d, now); err == nil {
		t.Fatal("expected rename failure")
	}
	files, _ := os.ReadDir(filepath.Join(root, "cache"))
	if len(files) != 1 {
		t.Fatal("abandoned temporary file", files)
	}
	if d.Comments[0].ID != "new" {
		t.Fatal("lost memory data")
	}
}

func TestCanonicalIdentityIgnoresCheckoutAndURLSpelling(t *testing.T) {
	p, d, now := fixture()
	c := New(t.TempDir())
	if err := c.Put(p, model.Comments, d, now); err != nil {
		t.Fatal(err)
	}
	q := p
	q.Host = "GITHUB.COM"
	q.Repository = "OWNER/REPO"
	q.URL = ""
	q.NodeID = "also-not-the-key"
	if _, ok, err := c.Get(q, model.Comments, now); err != nil || !ok {
		t.Fatal(ok, err)
	}
	q.Number++
	if _, ok, err := c.Get(q, model.Comments, now); err != nil || ok {
		t.Fatal(ok, err)
	}
}
func TestDiskUnavailableDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "cache"), []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	c := New(root)
	p, d, now := fixture()
	if err := c.Put(p, model.Comments, d, now); err == nil {
		t.Fatal("write accepted")
	}
	if _, ok, err := c.Get(p, model.Comments, now); err == nil || ok {
		t.Fatal(ok, err)
	}
}

func TestDiskUnrelatedCleanupFailureDoesNotBlockTarget(t *testing.T) {
	for _, kind := range []string{"unreadable", "missing"} {
		t.Run(kind, func(t *testing.T) {
			c := New(t.TempDir())
			p, d, now := fixture()
			if err := c.Put(p, model.Comments, d, now); err != nil {
				t.Fatal(err)
			}
			unrelated := filepath.Join(c.dir, "unrelated.json")
			if kind == "unreadable" {
				if err := os.WriteFile(unrelated, []byte("private"), 0000); err != nil {
					t.Fatal(err)
				}
				if _, err := os.ReadFile(unrelated); err == nil {
					t.Skip("process can read mode 0000 files")
				}
			} else {
				// A dangling link deterministically exercises ENOENT after ReadDir,
				// the same failure as another process removing a listed entry.
				if err := os.Symlink(filepath.Join(c.dir, "removed"), unrelated); err != nil {
					t.Fatal(err)
				}
			}
			if _, ok, err := c.Get(p, model.Comments, now); err != nil || !ok {
				t.Fatalf("unrelated cleanup blocked read: hit=%t error=%v", ok, err)
			}
			d.Comments[0].ID = "replacement"
			if err := c.Put(p, model.Comments, d, now); err != nil {
				t.Fatalf("unrelated cleanup blocked write: %v", err)
			}
			e, ok, err := c.Get(p, model.Comments, now)
			if err != nil || !ok || e.Data.Comments[0].ID != "replacement" {
				t.Fatal(e, ok, err)
			}
			if kind == "unreadable" {
				if err := os.Chmod(c.path(p, model.Comments), 0000); err != nil {
					t.Fatal(err)
				}
				if _, ok, err := c.Get(p, model.Comments, now); err == nil || ok {
					t.Fatal("requested read error was suppressed", ok, err)
				}
			}
		})
	}
}
