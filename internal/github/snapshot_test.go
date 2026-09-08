package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cupsadarius/herdr-pr-glance/internal/command"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

type reply struct {
	out string
	err error
}
type fixtureRunner struct {
	t       *testing.T
	replies []reply
	calls   [][]string
}

func (r *fixtureRunner) Run(ctx context.Context, cwd string, args ...string) (string, error) {
	r.t.Helper()
	if cwd != "/source" {
		r.t.Fatalf("cwd=%q", cwd)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	r.calls = append(r.calls, args)
	joined := strings.Join(args, " ")
	for _, forbidden := range []string{"body", "comments", "reviews", "reviewThreads"} {
		if strings.Contains(joined, forbidden) {
			r.t.Fatalf("summary requests %s", forbidden)
		}
	}
	if len(r.replies) == 0 {
		r.t.Fatal("unexpected command", args)
	}
	x := r.replies[0]
	r.replies = r.replies[1:]
	return x.out, x.err
}
func discovery(host, repo, state string, draft bool) string {
	return fmt.Sprintf(`{"id":"PR_node","url":"https://%s/%s/pull/7","number":7,"title":"Fix","state":%q,"isDraft":%t,"author":{"login":"alice"},"baseRefName":"main","headRefName":"topic","headRepository":{"name":"fork"},"headRepositoryOwner":{"login":"alice"},"additions":5,"deletions":2,"changedFiles":3,"reviewDecision":"APPROVED"}`, host, repo, state, draft)
}
func page(nodes string, more bool, cursor string) string {
	return fmt.Sprintf(`{"data":{"node":{"commits":{"totalCount":143,"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":%s,"pageInfo":{"hasNextPage":%t,"endCursor":%q}}}}}]}}}}`, nodes, more, cursor)
}
func TestSnapshotIdentityStatesAndExactTotals(t *testing.T) {
	for _, tc := range []struct {
		host, repo, state string
		draft             bool
	}{{"github.com", "base/repo", "OPEN", false}, {"ghe.example", "other/repo", "OPEN", true}, {"github.com", "third/repo", "MERGED", false}, {"github.com", "base/repo", "CLOSED", false}} {
		t.Run(tc.host+tc.repo+tc.state, func(t *testing.T) {
			r := &fixtureRunner{t: t, replies: []reply{{out: discovery(tc.host, tc.repo, tc.state, tc.draft)}, {out: page(`[]`, false, "")}}}
			now := time.Unix(100, 0)
			got, err := (Client{Runner: r, Now: func() time.Time { return now }}).Snapshot(context.Background(), model.Source{CWD: "/source", Branch: "topic"})
			if err != nil {
				t.Fatal(err)
			}
			if got.PR.Host != tc.host || got.PR.Repository != tc.repo || got.PR.NodeID != "PR_node" || got.PR.Number != 7 || got.State != tc.state || got.Draft != tc.draft || got.Commits != 143 || got.ChangedFiles != 3 || !got.FetchedAt.Equal(now) || got.Author != "alice" || got.HeadRepository != "alice/fork" {
				t.Fatalf("snapshot=%+v PR=%+v", got, got.PR)
			}
			if strings.Join(r.calls[0][:3], " ") != "gh pr view" || strings.Contains(strings.Join(r.calls[0], " "), "--repo") {
				t.Fatal(r.calls[0])
			}
			if !strings.Contains(strings.Join(r.calls[1], " "), "--hostname "+tc.host) {
				t.Fatal(r.calls[1])
			}
		})
	}
}
func TestChecksPaginationCountsAndOrdering(t *testing.T) {
	nodes := []map[string]string{}
	for _, s := range []string{"SUCCESS", "FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "NEUTRAL", "SKIPPED", "NEW_RESULT"} {
		nodes = append(nodes, map[string]string{"__typename": "CheckRun", "name": s, "status": "COMPLETED", "conclusion": s})
	}
	nodes = append(nodes, map[string]string{"__typename": "CheckRun", "name": "running", "status": "IN_PROGRESS"})
	b, _ := json.Marshal(nodes)
	r := &fixtureRunner{t: t, replies: []reply{{out: discovery("github.com", "o/r", "OPEN", false)}, {out: page(string(b), true, "next")}, {out: page(`[{"__typename":"StatusContext","context":"legacy","state":"ERROR","targetUrl":"https://ci.test"}]`, false, "")}}}
	got, err := (Client{Runner: r}).Snapshot(context.Background(), model.Source{CWD: "/source"})
	if err != nil {
		t.Fatal(err)
	}
	want := []model.CheckState{model.CheckFailed, model.CheckFailed, model.CheckTimedOut, model.CheckCancelled, model.CheckActionRequired, model.CheckPending, model.CheckPassed, model.CheckNeutral, model.CheckSkipped, model.CheckUnknown}
	if len(got.Checks) != len(want) {
		t.Fatal(got.Checks)
	}
	for i, s := range want {
		if got.Checks[i].State != s {
			t.Fatalf("check %d: %s want %s", i, got.Checks[i].State, s)
		}
	}
	if got.CheckCounts.Failed != 5 || got.CheckCounts.Pending != 1 || got.CheckCounts.Passed != 1 || got.CheckCounts.Neutral != 1 || got.CheckCounts.Skipped != 1 || got.CheckCounts.Unknown != 1 {
		t.Fatal(got.CheckCounts)
	}
	if !strings.Contains(strings.Join(r.calls[2], " "), "cursor=next") {
		t.Fatal(r.calls[2])
	}
}
func TestAbsenceAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, stderr string
		code         int
		absent       bool
		kind         model.ErrorKind
	}{{"absent", `no pull requests found for branch "topic"`, 1, true, ""}, {"auth", "authentication required", 4, false, model.AuthenticationError}, {"network", "error connecting to api.github.com", 1, false, model.NetworkError}, {"other", "no pull requests found for branch topic: server failure", 1, false, model.GitHubError}} {
		t.Run(tc.name, func(t *testing.T) {
			r := &fixtureRunner{t: t, replies: []reply{{err: &command.Error{Program: "gh", Stderr: tc.stderr, ExitCode: tc.code, Err: errors.New("exit")}}}}
			got, err := (Client{Runner: r}).Snapshot(context.Background(), model.Source{CWD: "/source", Branch: "topic"})
			if tc.absent {
				if err != nil || got.EmptyReason != model.NoPR || got.PR != nil {
					t.Fatalf("%+v %v", got, err)
				}
			} else {
				var e *model.FetchError
				if !errors.As(err, &e) || e.Kind != tc.kind {
					t.Fatalf("%v", err)
				}
			}
		})
	}
}
func TestGraphQLErrorRejectsPartialData(t *testing.T) {
	r := &fixtureRunner{t: t, replies: []reply{{out: discovery("github.com", "o/r", "OPEN", false)}, {out: `{"data":{"node":{"commits":{"totalCount":143}}},"errors":[{"message":"denied"}]}`}}}
	_, err := (Client{Runner: r}).Snapshot(context.Background(), model.Source{CWD: "/source"})
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatal(err)
	}
}
func TestCancellationStopsCommands(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &fixtureRunner{t: t}
	_, err := (Client{Runner: r}).Snapshot(ctx, model.Source{CWD: "/source"})
	if !errors.Is(err, context.Canceled) || len(r.calls) != 0 {
		t.Fatalf("%v %v", err, r.calls)
	}
}

func TestCommitTotalIndependentOfTruncatedRecords(t *testing.T) {
	var fixture map[string]any
	if err := json.Unmarshal([]byte(page(`[]`, false, "")), &fixture); err != nil {
		t.Fatal(err)
	}
	commits := fixture["data"].(map[string]any)["node"].(map[string]any)["commits"].(map[string]any)
	records := make([]any, 100)
	for i := range records {
		records[i] = map[string]any{"commit": map[string]any{"statusCheckRollup": nil}}
	}
	commits["nodes"] = records
	b, _ := json.Marshal(fixture)
	r := &fixtureRunner{t: t, replies: []reply{{out: discovery("github.com", "o/r", "OPEN", false)}, {out: string(b)}}}
	got, err := (Client{Runner: r}).Snapshot(context.Background(), model.Source{CWD: "/source"})
	if err != nil || got.Commits != 143 {
		t.Fatalf("commits=%d err=%v", got.Commits, err)
	}
}
func TestMalformedAndStalledResponses(t *testing.T) {
	for _, response := range []string{`{`, `{"data":null}`, `{"data":{"node":null}}`, `{"data":{"node":{"commits":{"totalCount":1,"nodes":[]}}}}`, page(`[]`, true, "")} {
		t.Run(response, func(t *testing.T) {
			r := &fixtureRunner{t: t, replies: []reply{{out: discovery("github.com", "o/r", "OPEN", false)}, {out: response}}}
			_, err := (Client{Runner: r}).Snapshot(context.Background(), model.Source{CWD: "/source"})
			var typed *model.FetchError
			if !errors.As(err, &typed) || typed.Kind != model.InvalidResponseError {
				t.Fatalf("%v", err)
			}
		})
	}
}

type cancelRunner struct {
	cancel context.CancelFunc
	calls  int
}

func (r *cancelRunner) Run(ctx context.Context, cwd string, args ...string) (string, error) {
	r.calls++
	r.cancel()
	return discovery("github.com", "o/r", "OPEN", false), nil
}
func TestCancellationBetweenDiscoveryAndChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &cancelRunner{cancel: cancel}
	_, err := (Client{Runner: r}).Snapshot(ctx, model.Source{CWD: "/source"})
	if !errors.Is(err, context.Canceled) || r.calls != 1 {
		t.Fatalf("%v calls=%d", err, r.calls)
	}
}

func TestGraphQLCancellationPreservedWithPartialOutput(t *testing.T) {
	r := &fixtureRunner{t: t, replies: []reply{{out: discovery("github.com", "o/r", "OPEN", false)}, {out: `{"data":null,"errors":[{"message":"incomplete"}]}`, err: context.Canceled}}}
	_, err := (Client{Runner: r}).Snapshot(context.Background(), model.Source{CWD: "/source"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestAbsenceResolvedHeadGrammar(t *testing.T) {
	for _, tc := range []struct {
		name, stderr string
		code         int
		absent       bool
	}{
		{"fork", `no pull requests found for branch "alice:topic"`, 1, true},
		{"upstream", `no pull requests found for branch "renamed-topic"`, 1, true},
		{"escaped quote", `no pull requests found for branch "topic\"quoted"`, 1, true},
		{"appended error", `no pull requests found for branch "topic": server failure`, 1, false},
		{"unclosed", `no pull requests found for branch "topic`, 1, false},
		{"bad escape", `no pull requests found for branch "topic\q"`, 1, false},
		{"raw quote", "no pull requests found for branch `topic`", 1, false},
		{"empty", `no pull requests found for branch ""`, 1, false},
		{"wrong exit", `no pull requests found for branch "topic"`, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &fixtureRunner{t: t, replies: []reply{{err: &command.Error{Program: "gh", Stderr: tc.stderr, ExitCode: tc.code, Err: errors.New("exit")}}}}
			got, err := (Client{Runner: r}).Snapshot(context.Background(), model.Source{CWD: "/source", Branch: "topic"})
			if tc.absent {
				if err != nil || got.EmptyReason != model.NoPR || got.PR != nil {
					t.Fatalf("snapshot=%+v err=%v", got, err)
				}
			} else {
				var e *model.FetchError
				if !errors.As(err, &e) || e.Kind != model.GitHubError {
					t.Fatalf("expected GitHubError, got %v", err)
				}
			}
		})
	}
}
