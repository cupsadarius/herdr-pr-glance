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
const checksQuery = `query($id:ID!,$cursor:String){node(id:$id){... on PullRequest{commits(last:1){totalCount nodes{commit{statusCheckRollup{contexts(first:100,after:$cursor){nodes{__typename ... on CheckRun{name status conclusion detailsUrl} ... on StatusContext{context state targetUrl}} pageInfo{hasNextPage endCursor}}}}}}}}}`

func (c Client) Snapshot(ctx context.Context, source model.Source) (model.Snapshot, error) {
	var result model.Snapshot
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if source.EmptyReason != "" {
		result.EmptyReason = source.EmptyReason
		result.FetchedAt = c.now()
		return result, nil
	}
	cwd := source.CWD
	if cwd == "" {
		cwd = source.Root
	}
	if cwd == "" {
		return result, invalid("missing source checkout")
	}
	out, err := c.Runner.Run(ctx, cwd, "gh", "pr", "view", "--json", discoveryFields)
	if err != nil {
		var e *command.Error
		if errors.As(err, &e) && e.ExitCode == 1 && isNoPRMessage(e.Stderr) {
			result.EmptyReason = model.NoPR
			result.FetchedAt = c.now()
			return result, nil
		}
		return result, fetchError(err)
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
	u, err := url.Parse(pr.URL)
	if err != nil {
		return result, invalid("invalid PR URL")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if u.Scheme != "https" || u.Host == "" || u.User != nil || len(parts) != 4 || parts[2] != "pull" || parts[3] != strconv.Itoa(pr.Number) || pr.Number < 1 || pr.ID == "" {
		return result, invalid("invalid PR identity")
	}
	result = model.Snapshot{PR: &model.PR{Host: u.Host, Repository: parts[0] + "/" + parts[1], Number: pr.Number, NodeID: pr.ID, URL: pr.URL}, Title: pr.Title, Author: pr.Author.Login, State: pr.State, Draft: pr.IsDraft, BaseBranch: pr.BaseRefName, HeadBranch: pr.HeadRefName, ReviewDecision: pr.ReviewDecision, Additions: pr.Additions, Deletions: pr.Deletions, ChangedFiles: pr.ChangedFiles}
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
		data, err := c.graphql(ctx, cwd, u.Host, checksQuery, vars...)
		if err != nil {
			return model.Snapshot{}, err
		}
		var response struct {
			Node *struct {
				Commits *struct {
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
