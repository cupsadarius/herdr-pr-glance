package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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

// The first discussion call succeeds only after a newer generation has been
// accepted for the same PR. Cancellation alone cannot prevent this completion.
type outOfOrderDiscussions struct {
	started chan context.Context
	release chan struct{}
	calls   int
}

func (a *outOfOrderDiscussions) Snapshot(context.Context, model.Source) (model.Snapshot, error) {
	return model.Snapshot{PR: &model.PR{Host: "github.com", Repository: "a/b", Number: 1}}, nil
}
func (a *outOfOrderDiscussions) SnapshotPR(context.Context, model.Source, model.PR) (model.Snapshot, error) {
	panic("unexpected pinned snapshot")
}
func (a *outOfOrderDiscussions) Discussion(ctx context.Context, pr model.PR, s model.Section) (model.Discussion, error) {
	a.calls++
	body := "new"
	if a.calls == 1 {
		a.started <- ctx
		<-a.release
		body = "old"
	}
	return model.Discussion{PR: pr, Section: s, Complete: true, Comments: []model.Comment{{Body: body}}}, nil
}
func TestObsoleteDiscussionCannotOverwriteSharedDisk(t *testing.T) {
	a := &outOfOrderDiscussions{started: make(chan context.Context, 1), release: make(chan struct{})}
	disk := cache.New(t.TempDir())
	now := time.Unix(1000, 0)
	m := New(nil, a, disk, func() time.Time { return now })
	finish(m, source(m, "main", true))
	read := apply(m, SelectSectionMsg(model.Comments))
	old := apply(m, read())
	done := make(chan tea.Msg, 1)
	go func() { done <- old() }()
	ctx := <-a.started
	finish(m, source(m, "other", true))
	finish(m, source(m, "main", true))
	finish(m, apply(m, SelectSectionMsg(model.Comments)))
	if ctx.Err() != context.Canceled {
		t.Fatal("old request not canceled")
	}
	close(a.release)
	finish(m, apply(m, <-done))
	if got := m.Discussions[model.Comments].Data.Comments[0].Body; got != "new" {
		t.Fatalf("visible body=%q", got)
	}
	entry, hit, err := disk.Get(*m.Snapshot.PR, model.Comments, now)
	if err != nil || !hit || entry.Data.Comments[0].Body != "new" {
		t.Fatalf("obsolete result overwrote disk: %+v hit=%v err=%v", entry, hit, err)
	}
	fresh := New(nil, a, disk, func() time.Time { return now })
	finish(fresh, source(fresh, "main", true))
	finish(fresh, apply(fresh, SelectSectionMsg(model.Comments)))
	if got := fresh.Discussions[model.Comments].Data.Comments[0].Body; got != "new" || a.calls != 2 {
		t.Fatalf("reopened cache body=%q requests=%d", got, a.calls)
	}
}
