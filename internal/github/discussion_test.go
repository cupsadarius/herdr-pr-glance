package github

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cupsadarius/herdr-pr-glance/internal/cache"
	"github.com/cupsadarius/herdr-pr-glance/internal/command"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

type discussionRunner struct {
	t         *testing.T
	responses []string
	calls     [][]string
}

func (r *discussionRunner) Run(ctx context.Context, cwd string, args ...string) (string, error) {
	if cwd != "" {
		r.t.Fatalf("cwd=%q", cwd)
	}
	r.calls = append(r.calls, args)
	if len(r.responses) == 0 {
		r.t.Fatal("unexpected request")
	}
	s := r.responses[0]
	r.responses = r.responses[1:]
	return s, nil
}
func discussionPage(field, nodes string, more bool, cursor string) string {
	return fmt.Sprintf(`{"data":{"node":{"%s":{"nodes":%s,"pageInfo":{"hasNextPage":%t,"endCursor":%q}}}}}`, field, nodes, more, cursor)
}
func TestDiscussionCompletePagination(t *testing.T) {
	r := &discussionRunner{t: t, responses: []string{
		discussionPage("reviews", `[{"id":"r1","state":"APPROVED","body":"ok","submittedAt":"2026-01-01T00:00:00Z","author":{"login":"bob"},"url":"https://example/r1"}]`, true, "review-next"),
		discussionPage("reviews", `[{"id":"r2","state":"PENDING"},{"id":"r3","state":"COMMENTED"}]`, false, ""),
		discussionPage("reviewThreads", `[{"id":"t1","path":"a.go","line":null,"isResolved":true,"isOutdated":true}]`, true, "thread-next"),
		discussionPage("reviewThreads", `[{"id":"t2","path":"b.go","line":4}]`, false, ""),
		discussionPage("comments", `[{"id":"c1","author":null,"body":"hello\nworld","url":"https://example/c1","createdAt":"2026-01-01T00:00:00Z"}]`, true, "reply-next"),
		discussionPage("comments", `[{"id":"c2"}]`, true, "reply-next2"),
		discussionPage("comments", `[{"id":"c3"}]`, false, ""),
		discussionPage("comments", `[{"id":"c4"}]`, false, ""),
	}}
	d, err := (Client{Runner: r}).Discussion(context.Background(), model.PR{Host: "github.com", NodeID: "pr"}, model.Reviews)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Complete || len(d.Reviews) != 2 || len(d.Threads) != 2 {
		t.Fatalf("%+v", d)
	}
	resolved := d.Threads[1]
	if !resolved.Resolved || !resolved.Outdated || resolved.Line != nil || len(resolved.Comments) != 3 || resolved.Comments[0].Author != "[deleted]" || resolved.URL != "https://example/c1" {
		t.Fatalf("%+v", resolved)
	}
	for i, want := range map[int]string{1: "cursor=review-next", 3: "cursor=thread-next", 5: "cursor=reply-next", 6: "cursor=reply-next2"} {
		if !strings.Contains(strings.Join(r.calls[i], " "), want) {
			t.Fatalf("call %d: %v", i, r.calls[i])
		}
	}
}
func TestCommentsFailureNeverComplete(t *testing.T) {
	r := &discussionRunner{t: t, responses: []string{discussionPage("comments", `[{"id":"a"}]`, true, "next"), `{"errors":[{"message":"later page failed"}]}`}}
	d, err := (Client{Runner: r}).Discussion(context.Background(), model.PR{Host: "github.com", NodeID: "pr"}, model.Comments)
	if err == nil || d.Complete {
		t.Fatalf("%+v %v", d, err)
	}
}
func TestCommentsPagination(t *testing.T) {
	r := &discussionRunner{t: t, responses: []string{discussionPage("comments", `[{"id":"a"}]`, true, "next"), discussionPage("comments", `[{"id":"b"}]`, false, "")}}
	d, err := (Client{Runner: r}).Discussion(context.Background(), model.PR{Host: "github.com", NodeID: "pr"}, model.Comments)
	if err != nil || !d.Complete || len(d.Comments) != 2 {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestDiscussionRejectsMalformedPagination(t *testing.T) {
	for _, response := range []string{
		`{"data":{"node":null}}`,
		`{"data":{"node":{"comments":{"nodes":[]}}}}`,
		`{"data":{"node":{"comments":{"nodes":[],"pageInfo":{}}}}}`,
		`{"data":{"node":{"comments":{"pageInfo":{"hasNextPage":false}}}}}`,
		discussionPage("comments", `[null]`, false, ""),
		discussionPage("comments", `[]`, true, ""),
	} {
		t.Run(response, func(t *testing.T) {
			r := &discussionRunner{t: t, responses: []string{response}}
			d, err := (Client{Runner: r}).Discussion(context.Background(), model.PR{Host: "github.com", NodeID: "pr"}, model.Comments)
			if err == nil || d.Complete {
				t.Fatalf("%+v %v", d, err)
			}
		})
	}
}
func TestReplyFailurePreservesCompleteCache(t *testing.T) {
	p := model.PR{Host: "github.com", Repository: "o/r", Number: 1, NodeID: "pr"}
	now := time.Unix(1000, 0)
	disk := cache.New(t.TempDir())
	old := model.Discussion{PR: p, Section: model.Reviews, Complete: true, Reviews: []model.Review{{State: "APPROVED"}}}
	if err := disk.Put(p, model.Reviews, old, now); err != nil {
		t.Fatal(err)
	}
	r := &discussionRunner{t: t, responses: []string{discussionPage("reviews", `[]`, false, ""), discussionPage("reviewThreads", `[{"id":"thread"}]`, false, ""), discussionPage("comments", `[{"id":"reply"}]`, true, "next"), `{"errors":[{"message":"reply page failure"}]}`}}
	d, err := (Client{Runner: r}).Discussion(context.Background(), p, model.Reviews)
	if err == nil || d.Complete {
		t.Fatal(d, err)
	}
	if err := disk.Put(p, model.Reviews, d, now); err == nil {
		t.Fatal("accepted partial")
	}
	e, ok, err := disk.Get(p, model.Reviews, now)
	if err != nil || !ok || len(e.Data.Reviews) != 1 {
		t.Fatal(e, ok, err)
	}
}

func TestLiveDiscussion(t *testing.T) {
	cwd := os.Getenv("GLANCE_LIVE_PR_CWD")
	if cwd == "" {
		t.Skip("set GLANCE_LIVE_PR_CWD to an authorized public checkout")
	}
	c := Client{Runner: command.ExecRunner{}}
	s, err := c.Snapshot(context.Background(), model.Source{CWD: cwd, Branch: "fix/windows-endpoint-paste"})
	if err != nil {
		t.Fatal(err)
	}
	if s.PR == nil {
		t.Fatal("no PR")
	}
	for _, section := range []model.Section{model.Comments, model.Reviews} {
		d, err := c.Discussion(context.Background(), *s.PR, section)
		if err != nil || !d.Complete {
			t.Fatal(d, err)
		}
		t.Logf("%s comments=%d reviews=%d threads=%d complete=%t", section, len(d.Comments), len(d.Reviews), len(d.Threads), d.Complete)
	}
}

func TestDiscussionRejectsRepeatedCursor(t *testing.T) {
	r := &discussionRunner{t: t, responses: []string{discussionPage("comments", `[]`, true, "same"), discussionPage("comments", `[]`, true, "same")}}
	d, err := (Client{Runner: r}).Discussion(context.Background(), model.PR{Host: "github.com", NodeID: "pr"}, model.Comments)
	if err == nil || d.Complete {
		t.Fatal(d, err)
	}
}
