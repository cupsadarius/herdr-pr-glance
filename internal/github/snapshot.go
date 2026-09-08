package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/cupsadarius/herdr-pr-glance/internal/command"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

const discoveryFields = "id,url,number,title,author,state,isDraft,baseRefName,headRefName,headRepository,headRepositoryOwner,additions,deletions,changedFiles,reviewDecision"

// stackFields ride the checks query so a summary stays one GraphQL request.
// Both fields are null for a pull request that belongs to no stack, and the
// entry page is deliberately not followed: Size records the real total.
const stackEntries = 50
const stackFields = `stackEntry{position} stack{number size baseRefName entries(first:50){nodes{position pullRequest{id number url title state isDraft headRefName baseRefName reviewDecision}}}}`
const checksQuery = `query($id:ID!,$cursor:String){node(id:$id){... on PullRequest{` + stackFields + ` commits(last:1){totalCount nodes{commit{statusCheckRollup{contexts(first:100,after:$cursor){nodes{__typename ... on CheckRun{name status conclusion detailsUrl} ... on StatusContext{context state targetUrl}} pageInfo{hasNextPage endCursor}}}}}}}}}`

// Snapshot reports the pull request of the source checkout's branch.
func (c Client) Snapshot(ctx context.Context, source model.Source) (model.Snapshot, error) {
	cwd, early, err := c.begin(ctx, source)
	if early != nil || err != nil {
		return earlySnapshot(early), err
	}
	return c.snapshot(ctx, cwd, "gh", "pr", "view", "--json", discoveryFields)
}

// SnapshotPR reports one named pull request instead of the branch's own, so a
// pinned entry of a stack refreshes on the same cadence. gh resolves a number
// against the source checkout's repository.
func (c Client) SnapshotPR(ctx context.Context, source model.Source, pr model.PR) (model.Snapshot, error) {
	cwd, early, err := c.begin(ctx, source)
	if early != nil || err != nil {
		return earlySnapshot(early), err
	}
	return c.snapshot(ctx, cwd, "gh", "pr", "view", strconv.Itoa(pr.Number), "--json", discoveryFields)
}

// begin resolves the checkout to run gh in, or reports the snapshot to return
// when the source names no pull request to fetch at all.
func (c Client) begin(ctx context.Context, source model.Source) (string, *model.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if source.EmptyReason != "" {
		return "", &model.Snapshot{EmptyReason: source.EmptyReason, FetchedAt: c.now()}, nil
	}
	cwd := source.CWD
	if cwd == "" {
		cwd = source.Root
	}
	if cwd == "" {
		return "", nil, invalid("missing source checkout")
	}
	return cwd, nil, nil
}

func earlySnapshot(s *model.Snapshot) model.Snapshot {
	if s == nil {
		return model.Snapshot{}
	}
	return *s
}

func (c Client) snapshot(ctx context.Context, cwd string, discoveryArgs ...string) (model.Snapshot, error) {
	var result model.Snapshot
	out, err := c.Runner.Run(ctx, cwd, discoveryArgs...)
	if err != nil {
		var e *command.Error
		if errors.As(err, &e) && e.ExitCode == 1 && isNoPRMessage(e.Stderr) {
			result.EmptyReason = model.NoPR
			result.FetchedAt = c.now()
			return result, nil
		}
		return result, c.fetchError(err)
	}
	var pr struct {
		ID, URL, Title, State, BaseRefName, HeadRefName, ReviewDecision string
		Number, Additions, Deletions, ChangedFiles                      int
		IsDraft                                                         bool
		Author, HeadRepositoryOwner                                     struct{ Login string }
		HeadRepository                                                  struct{ Name string }
	}
	if err := json.Unmarshal([]byte(out), &pr); err != nil {
		return result, invalid("decode PR: %v", err)
	}
	identity, err := parseIdentity(pr.URL, pr.ID, pr.Number)
	if err != nil {
		return result, err
	}
	result = model.Snapshot{PR: &identity, Title: pr.Title, Author: pr.Author.Login, State: pr.State, Draft: pr.IsDraft, BaseBranch: pr.BaseRefName, HeadBranch: pr.HeadRefName, ReviewDecision: pr.ReviewDecision, Additions: pr.Additions, Deletions: pr.Deletions, ChangedFiles: pr.ChangedFiles}
	if pr.HeadRepository.Name != "" {
		result.HeadRepository = pr.HeadRepositoryOwner.Login + "/" + pr.HeadRepository.Name
	}
	cursor := ""
	seen := map[string]bool{}
	for {
		vars := []string{"id=" + pr.ID}
		if cursor != "" {
			vars = append(vars, "cursor="+cursor)
		}
		data, err := c.graphql(ctx, cwd, identity.Host, checksQuery, vars...)
		if err != nil {
			return model.Snapshot{}, err
		}
		var response struct {
			Node *struct {
				StackEntry *struct{ Position int }
				Stack      *stackNode
				Commits    *struct {
					TotalCount int
					Nodes      []struct {
						Commit struct {
							StatusCheckRollup *struct {
								Contexts struct {
									Nodes    []checkNode
									PageInfo struct {
										HasNextPage bool
										EndCursor   string
									}
								}
							}
						}
					}
				}
			}
		}
		if err := json.Unmarshal(data, &response); err != nil {
			return model.Snapshot{}, invalid("decode checks: %v", err)
		}
		if response.Node == nil || response.Node.Commits == nil {
			return model.Snapshot{}, invalid("missing PR commit connection")
		}
		if cursor == "" {
			if response.Node.StackEntry != nil {
				result.StackPosition = response.Node.StackEntry.Position
			}
			if response.Node.Stack != nil {
				stack, err := normalizeStack(*response.Node.Stack)
				if err != nil {
					return model.Snapshot{}, err
				}
				result.Stack = stack
			}
		}
		commits := response.Node.Commits
		result.Commits = commits.TotalCount
		if len(commits.Nodes) == 0 {
			if commits.TotalCount > 0 {
				return model.Snapshot{}, invalid("missing latest commit")
			}
			break
		}
		rollup := commits.Nodes[len(commits.Nodes)-1].Commit.StatusCheckRollup
		if rollup == nil {
			break
		}
		for _, n := range rollup.Contexts.Nodes {
			check := normalizeCheck(n)
			result.Checks = append(result.Checks, check)
			countCheck(&result.CheckCounts, check.State)
		}
		info := rollup.Contexts.PageInfo
		if !info.HasNextPage {
			break
		}
		if info.EndCursor == "" || seen[info.EndCursor] {
			return model.Snapshot{}, invalid("check pagination did not advance")
		}
		seen[info.EndCursor] = true
		cursor = info.EndCursor
	}
	sort.SliceStable(result.Checks, func(i, j int) bool {
		a, b := result.Checks[i], result.Checks[j]
		if checkRank(a.State) != checkRank(b.State) {
			return checkRank(a.State) < checkRank(b.State)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	result.FetchedAt = c.now()
	return result, nil
}

// gh reports the resolved head, which can differ from the local branch.
// Unquote consumes the complete quoted suffix and rejects appended errors.
func isNoPRMessage(message string) bool {
	quoted, ok := strings.CutPrefix(message, "no pull requests found for branch ")
	if !ok || !strings.HasPrefix(quoted, `"`) {
		return false
	}
	head, err := strconv.Unquote(quoted)
	return err == nil && head != ""
}

// parseIdentity rebuilds a pull request identity from the URL GitHub reports,
// the only place the host and repository are stated together.
func parseIdentity(rawURL, nodeID string, number int) (model.PR, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return model.PR{}, invalid("invalid PR URL")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if u.Scheme != "https" || u.Host == "" || u.User != nil || len(parts) != 4 || parts[2] != "pull" || parts[3] != strconv.Itoa(number) || number < 1 || nodeID == "" {
		return model.PR{}, invalid("invalid PR identity")
	}
	return model.PR{Host: u.Host, Repository: parts[0] + "/" + parts[1], Number: number, NodeID: nodeID, URL: rawURL}, nil
}

type stackNode struct {
	Number, Size int
	BaseRefName  string
	Entries      struct {
		Nodes []struct {
			Position    int
			PullRequest struct {
				ID, URL, Title, State, HeadRefName, BaseRefName, ReviewDecision string
				Number                                                          int
				IsDraft                                                         bool
			}
		}
	}
}

func normalizeStack(n stackNode) (*model.Stack, error) {
	stack := &model.Stack{Number: n.Number, Size: n.Size, BaseBranch: n.BaseRefName}
	for _, e := range n.Entries.Nodes {
		identity, err := parseIdentity(e.PullRequest.URL, e.PullRequest.ID, e.PullRequest.Number)
		if err != nil {
			return nil, err
		}
		stack.Entries = append(stack.Entries, model.StackEntry{Position: e.Position, PR: identity,
			Title: e.PullRequest.Title, State: e.PullRequest.State, HeadBranch: e.PullRequest.HeadRefName,
			BaseBranch: e.PullRequest.BaseRefName, ReviewDecision: e.PullRequest.ReviewDecision, Draft: e.PullRequest.IsDraft})
	}
	if len(stack.Entries) > stackEntries {
		stack.Entries = stack.Entries[:stackEntries]
	}
	sort.SliceStable(stack.Entries, func(i, j int) bool { return stack.Entries[i].Position < stack.Entries[j].Position })
	if stack.Size < len(stack.Entries) {
		stack.Size = len(stack.Entries)
	}
	return stack, nil
}

type checkNode struct {
	Type                                                            string `json:"__typename"`
	Name, Status, Conclusion, DetailsURL, Context, State, TargetURL string
}

func normalizeCheck(n checkNode) model.Check {
	c := model.Check{Name: n.Name, URL: n.DetailsURL, Status: n.Status, Conclusion: n.Conclusion, State: model.CheckUnknown}
	if n.Type == "StatusContext" {
		c.Name = n.Context
		c.URL = n.TargetURL
		c.Status = n.State
		c.Conclusion = n.State
		switch n.State {
		case "SUCCESS":
			c.State = model.CheckPassed
		case "FAILURE", "ERROR":
			c.State = model.CheckFailed
		case "PENDING", "EXPECTED":
			c.State = model.CheckPending
		}
		return c
	}
	if n.Type != "CheckRun" {
		return c
	}
	switch n.Status {
	case "QUEUED", "IN_PROGRESS", "WAITING", "REQUESTED", "PENDING":
		c.State = model.CheckPending
		return c
	case "COMPLETED":
	default:
		return c
	}
	switch n.Conclusion {
	case "SUCCESS":
		c.State = model.CheckPassed
	case "FAILURE", "STARTUP_FAILURE", "STALE":
		c.State = model.CheckFailed
	case "TIMED_OUT":
		c.State = model.CheckTimedOut
	case "CANCELLED":
		c.State = model.CheckCancelled
	case "ACTION_REQUIRED":
		c.State = model.CheckActionRequired
	case "NEUTRAL":
		c.State = model.CheckNeutral
	case "SKIPPED":
		c.State = model.CheckSkipped
	}
	return c
}
func checkRank(s model.CheckState) int {
	switch s {
	case model.CheckFailed:
		return 0
	case model.CheckTimedOut:
		return 1
	case model.CheckCancelled:
		return 2
	case model.CheckActionRequired:
		return 3
	case model.CheckPending:
		return 4
	case model.CheckPassed:
		return 5
	case model.CheckNeutral:
		return 6
	case model.CheckSkipped:
		return 7
	default:
		return 8
	}
}
func countCheck(c *model.CheckCounts, s model.CheckState) {
	switch s {
	case model.CheckFailed, model.CheckTimedOut, model.CheckCancelled, model.CheckActionRequired:
		c.Failed++
	case model.CheckPending:
		c.Pending++
	case model.CheckPassed:
		c.Passed++
	case model.CheckNeutral:
		c.Neutral++
	case model.CheckSkipped:
		c.Skipped++
	default:
		c.Unknown++
	}
}
