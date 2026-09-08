package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cupsadarius/herdr-pr-glance/internal/cache"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

func TestDiskCacheIntegration(t *testing.T) {
	for _, mode := range []string{"fresh", "expired", "corrupt", "unwritable"} {
		t.Run(mode, func(t *testing.T) {
			m, a, _, now := harness()
			dir := t.TempDir()
			disk := cache.New(dir)
			m.cache = disk
			finish(m, source(m, "main", true))
			pr := *m.Snapshot.PR
			stamp := *now
			if mode == "expired" {
				stamp = stamp.Add(-301 * time.Second)
			}
			if mode == "fresh" || mode == "expired" {
				if err := disk.Put(pr, model.Reviews, model.Discussion{PR: pr, Section: model.Reviews, Complete: true, Reviews: []model.Review{{State: "APPROVED"}}}, stamp); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "corrupt" {
				if err := disk.Put(pr, model.Reviews, model.Discussion{PR: pr, Section: model.Reviews, Complete: true}, stamp); err != nil {
					t.Fatal(err)
				}
				paths, _ := filepath.Glob(filepath.Join(dir, "cache", "*.json"))
				if err := os.WriteFile(paths[0], []byte("broken"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "unwritable" {
				if err := os.WriteFile(filepath.Join(dir, "cache"), []byte("blocks directory"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			finish(m, apply(m, SelectSectionMsg(model.Reviews)))
			state := m.Discussions[model.Reviews]
			if state.Data == nil || state.Error != nil {
				t.Fatalf("discussion unavailable: %+v", state)
			}
			if mode == "fresh" {
				if a.discussions != 0 || state.Data.Reviews[0].State != "APPROVED" {
					t.Fatal("fresh disk data not used")
				}
			} else if a.discussions != 1 {
				t.Fatal("cache fallback failed")
			}
			if (mode == "corrupt" || mode == "unwritable") && state.Warning == nil {
				t.Fatal("cache warning hidden")
			}
			if mode == "unwritable" {
				finish(m, apply(m, SelectSectionMsg(model.Overview)))
				finish(m, apply(m, SelectSectionMsg(model.Reviews)))
				if a.discussions != 1 {
					t.Fatal("fresh memory ignored after disk write failure")
				}
			}
		})
	}
}
