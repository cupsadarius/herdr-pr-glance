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
func (r *discussionRunner) call(i int) string { return strings.Join(r.calls[i], " ") }

func connectionJSON(nodes string, more bool, cursor string) string {
	return fmt.Sprintf(`{"nodes":%s,"pageInfo":{"hasNextPage":%t,"endCursor":%q}}`, nodes, more, cursor)
}
func discussionPage(field, nodes string, more bool, cursor string) string {
	return fmt.Sprintf(`{"data":{"node":{%q:%s}}}`, field, connectionJSON(nodes, more, cursor))
}

// reviewsPage renders the batched reviews query response; an empty section is omitted
// exactly as the exhausted connection is dropped from the follow-up query.
func reviewsPage(reviews, threads string) string {
	parts := []string{}
	if reviews != "" {
		parts = append(parts, `"reviews":`+reviews)
	}
	if threads != "" {
		parts = append(parts, `"reviewThreads":`+threads)
	}
	return `{"data":{"node":{` + strings.Join(parts, ",") + `}}}`
}
func threadNode(id, fields, comments string, more bool, cursor string) string {
	return fmt.Sprintf(`{"id":%q%s,"comments":%s}`, id, fields, connectionJSON(comments, more, cursor))
}
func threadsPage(more bool, cursor string, nodes ...string) string {
	return connectionJSON("["+strings.Join(nodes, ",")+"]", more, cursor)
}
func noReviews() string { return connectionJSON(`[]`, false, "") }
func commentNodes(ids ...string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf(`{"id":%q,"url":"https://example/%s"}`, id, id))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// The N+1 regression: nothing reports hasNextPage, so nothing may be fetched twice.
func TestReviewsFetchesEverythingInOneRequest(t *testing.T) {
	reviews := connectionJSON(`[{"id":"r1","state":"APPROVED"},{"id":"r2","state":"COMMENTED"},{"id":"r3","state":"DISMISSED"}]`, false, "")
	threads := threadsPage(false, "",
		threadNode("t1", `,"isResolved":true`, commentNodes("c1", "c2"), false, ""),
		threadNode("t2", `,"path":"b.go"`, commentNodes("c3", "c4"), false, ""))
	r := &discussionRunner{t: t, responses: []string{reviewsPage(reviews, threads)}}
	d, err := (Client{Runner: r}).Discussion(context.Background(), model.PR{Host: "github.com", NodeID: "pr"}, model.Reviews)
	if err != nil || !d.Complete || len(d.Reviews) != 3 || len(d.Threads) != 2 {
		t.Fatalf("%+v %v", d, err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("want 1 request, got %d: %v", len(r.calls), r.calls)
	}
	if d.Threads[0].ID != "t2" || d.Threads[0].Path != "b.go" || len(d.Threads[0].Comments) != 2 || d.Threads[0].URL != "https://example/c3" {
		t.Fatalf("%+v", d.Threads[0])
	}
	if d.Threads[1].ID != "t1" || !d.Threads[1].Resolved || len(d.Threads[1].Comments) != 2 {
		t.Fatalf("%+v", d.Threads[1])
	}
}

// An exhausted reviews connection must not be re-requested alongside thread page two.
func TestReviewsFollowUpPagesThreadsOnly(t *testing.T) {
	first := reviewsPage(
		connectionJSON(`[{"id":"r1","state":"APPROVED"}]`, false, ""),
		threadsPage(true, "thread-next", threadNode("t1", "", commentNodes("c1"), false, "")))
	second := reviewsPage("", threadsPage(false, "", threadNode("t2", "", commentNodes("c2"), false, "")))
	r := &discussionRunner{t: t, responses: []string{first, second}}
	d, err := (Client{Runner: r}).Discussion(context.Background(), model.PR{Host: "github.com", NodeID: "pr"}, model.Reviews)
	if err != nil || !d.Complete || len(d.Reviews) != 1 || len(d.Threads) != 2 {
		t.Fatalf("%+v %v", d, err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("want 2 requests, got %d: %v", len(r.calls), r.calls)
	}
	follow := r.call(1)
	if strings.Contains(follow, "reviews(first") || !strings.Contains(follow, "reviewThreads(first") {
		t.Fatalf("follow-up still pages reviews: %s", follow)
	}
	if strings.Contains(follow, "$reviewCursor") {
		t.Fatalf("follow-up declares an unused variable: %s", follow)
	}
	if !strings.Contains(follow, "threadCursor=thread-next") {
		t.Fatalf("missing thread cursor: %s", follow)
	}
}

// Only the thread whose nested comments overflow costs an extra request.
func TestReviewsFetchesReplyOverflowPerThreadOnly(t *testing.T) {
	threads := threadsPage(false, "",
		threadNode("t1", "", commentNodes("c1"), true, "reply-next"),
		threadNode("t2", "", commentNodes("c2"), false, ""))
	r := &discussionRunner{t: t, responses: []string{
		reviewsPage(noReviews(), threads),
		discussionPage("comments", commentNodes("c3"), false, ""),
	}}
	d, err := (Client{Runner: r}).Discussion(context.Background(), model.PR{Host: "github.com", NodeID: "pr"}, model.Reviews)
	if err != nil || !d.Complete || len(d.Threads) != 2 {
		t.Fatalf("%+v %v", d, err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("want 2 requests, got %d: %v", len(r.calls), r.calls)
	}
	overflow := r.call(1)
	if !strings.Contains(overflow, "id=t1") || !strings.Contains(overflow, "cursor=reply-next") {
		t.Fatalf("unexpected overflow request: %s", overflow)
	}
	if len(d.Threads[0].Comments) != 2 || d.Threads[0].ID != "t1" || len(d.Threads[1].Comments) != 1 {
		t.Fatalf("%+v", d.Threads)
	}
}

func TestDiscussionCompletePagination(t *testing.T) {
	firstThread := threadNode("t1", `,"path":"a.go","line":null,"isResolved":true,"isOutdated":true`,
		`[{"id":"c1","author":null,"body":"hello\nworld","url":"https://example/c1","createdAt":"2026-01-01T00:00:00Z"}]`, true, "reply-next")
	firstReview := `[{"id":"r1","state":"APPROVED","body":"ok","submittedAt":"2026-01-01T00:00:00Z",` +
		`"author":{"login":"bob"},"url":"https://example/r1"}]`
	r := &discussionRunner{t: t, responses: []string{
		reviewsPage(connectionJSON(firstReview, true, "review-next"), threadsPage(true, "thread-next", firstThread)),
		reviewsPage(
			connectionJSON(`[{"id":"r2","state":"PENDING"},{"id":"r3","state":"COMMENTED"}]`, false, ""),
			threadsPage(false, "", threadNode("t2", `,"path":"b.go","line":4`, commentNodes("c9"), false, ""))),
		discussionPage("comments", `[{"id":"c2"}]`, true, "reply-next2"),
		discussionPage("comments", `[{"id":"c3"}]`, false, ""),
	}}
	d, err := (Client{Runner: r}).Discussion(context.Background(), model.PR{Host: "github.com", NodeID: "pr"}, model.Reviews)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Complete || len(d.Reviews) != 2 || len(d.Threads) != 2 {
		t.Fatalf("%+v", d)
	}
	if d.Reviews[0].Author != "bob" || !d.Reviews[0].CreatedAt.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("%+v", d.Reviews[0])
	}
	resolved := d.Threads[1]
	if !resolved.Resolved || !resolved.Outdated || resolved.Line != nil || len(resolved.Comments) != 3 ||
		resolved.Comments[0].Author != "[deleted]" || resolved.URL != "https://example/c1" {
		t.Fatalf("%+v", resolved)
	}
	if len(r.calls) != 4 {
		t.Fatalf("want 4 requests, got %d: %v", len(r.calls), r.calls)
	}
	for i, want := range map[int][]string{
		1: {"reviewCursor=review-next", "threadCursor=thread-next"},
		2: {"id=t1", "cursor=reply-next"},
		3: {"cursor=reply-next2"},
	} {
		for _, w := range want {
			if !strings.Contains(r.call(i), w) {
				t.Fatalf("call %d missing %q: %v", i, w, r.calls[i])
			}
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
	if len(r.calls) != 2 || strings.Contains(r.call(0), "reviewThreads") {
		t.Fatalf("%v", r.calls)
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

func TestReviewsRejectsMalformedBatch(t *testing.T) {
	for name, response := range map[string]string{
		"missing threads":        reviewsPage(noReviews(), ""),
		"missing reviews":        reviewsPage("", threadsPage(false, "")),
		"null thread":            reviewsPage(noReviews(), threadsPage(false, "", "null")),
		"thread without id":      reviewsPage(noReviews(), threadsPage(false, "", `{"path":"a.go"}`)),
		"reply without pageInfo": reviewsPage(noReviews(), threadsPage(false, "", `{"id":"t1","comments":{"nodes":[]}}`)),
		"reply without id":       reviewsPage(noReviews(), threadsPage(false, "", threadNode("t1", "", `[{"body":"hi"}]`, false, ""))),
		"empty thread cursor":    reviewsPage(noReviews(), threadsPage(true, "")),
	} {
		t.Run(name, func(t *testing.T) {
			r := &discussionRunner{t: t, responses: []string{response}}
			d, err := (Client{Runner: r}).Discussion(context.Background(), model.PR{Host: "github.com", NodeID: "pr"}, model.Reviews)
			if err == nil || d.Complete {
				t.Fatalf("%+v %v", d, err)
			}
		})
	}
}

func TestReviewsThreadPageFailureNeverComplete(t *testing.T) {
	r := &discussionRunner{t: t, responses: []string{
		reviewsPage(noReviews(), threadsPage(true, "next", threadNode("t1", "", commentNodes("c1"), false, ""))),
		`{"errors":[{"message":"thread page failed"}]}`,
	}}
	d, err := (Client{Runner: r}).Discussion(context.Background(), model.PR{Host: "github.com", NodeID: "pr"}, model.Reviews)
	if err == nil || d.Complete {
		t.Fatalf("%+v %v", d, err)
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
	r := &discussionRunner{t: t, responses: []string{
		reviewsPage(noReviews(), threadsPage(false, "", threadNode("thread", "", commentNodes("reply"), true, "next"))),
		`{"errors":[{"message":"reply page failure"}]}`,
	}}
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
