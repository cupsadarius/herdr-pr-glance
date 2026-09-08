package github

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

const commentFields = `id body url createdAt author { login }`

type discussionComment struct {
	ID, Body, URL, State   string
	CreatedAt, SubmittedAt time.Time
	Author                 *struct{ Login string }
}

func (c discussionComment) comment() model.Comment {
	author := "[deleted]"
	if c.Author != nil {
		author = c.Author.Login
	}
	return model.Comment{ID: c.ID, Author: author, Body: c.Body, URL: c.URL, CreatedAt: c.CreatedAt}
}

// Discussion retrieves every page of one section. Fully addressed node queries
// do not depend on the shell's checkout; source discovery happens in Snapshot.
// Errors return an incomplete result that must never replace a complete cache.
func (c Client) Discussion(ctx context.Context, pr model.PR, section model.Section) (model.Discussion, error) {
	d := model.Discussion{PR: pr, Section: section}
	if pr.NodeID == "" || pr.Host == "" {
		return d, invalid("discussion requires PR host and node ID")
	}
	switch section {
	case model.Comments:
		nodes, err := c.discussionPages(ctx, pr.Host, pr.NodeID, "PullRequest", "comments", commentFields)
		if err != nil {
			return d, err
		}
		for _, raw := range nodes {
			var v discussionComment
			if err := json.Unmarshal(raw, &v); err != nil {
				return d, invalid("decode comment: %v", err)
			}
			d.Comments = append(d.Comments, v.comment())
		}
	case model.Reviews:
		nodes, err := c.discussionPages(ctx, pr.Host, pr.NodeID, "PullRequest", "reviews", `id body url submittedAt state author { login }`)
		if err != nil {
			return d, err
		}
		for _, raw := range nodes {
			var v discussionComment
			if err := json.Unmarshal(raw, &v); err != nil {
				return d, invalid("decode review: %v", err)
			}
			if v.State == "PENDING" {
				continue
			}
			v.CreatedAt = v.SubmittedAt
			d.Reviews = append(d.Reviews, model.Review{Comment: v.comment(), State: v.State})
		}
		nodes, err = c.discussionPages(ctx, pr.Host, pr.NodeID, "PullRequest", "reviewThreads", `id path line isResolved isOutdated`)
		if err != nil {
			return d, err
		}
		for _, raw := range nodes {
			var v struct {
				ID, Path               string
				Line                   *int
				IsResolved, IsOutdated bool
			}
			if err := json.Unmarshal(raw, &v); err != nil {
				return d, invalid("decode thread: %v", err)
			}
			thread := model.ReviewThread{ID: v.ID, Path: v.Path, Line: v.Line, Resolved: v.IsResolved, Outdated: v.IsOutdated}
			replies, err := c.discussionPages(ctx, pr.Host, v.ID, "PullRequestReviewThread", "comments", commentFields)
			if err != nil {
				return d, err
			}
			for _, raw := range replies {
				var comment discussionComment
				if err := json.Unmarshal(raw, &comment); err != nil {
					return d, invalid("decode reply: %v", err)
				}
				thread.Comments = append(thread.Comments, comment.comment())
			}
			if len(thread.Comments) > 0 {
				thread.URL = thread.Comments[0].URL
			}
			d.Threads = append(d.Threads, thread)
		}
		sort.SliceStable(d.Threads, func(i, j int) bool { return !d.Threads[i].Resolved && d.Threads[j].Resolved })
	default:
		return d, invalid("unsupported discussion section %q", section)
	}
	d.Complete = true
	d.FetchedAt = c.now()
	return d, nil
}

// Each connection owns its cursor, including every thread's reply connection.
func (c Client) discussionPages(ctx context.Context, host, id, nodeType, field, fields string) ([]json.RawMessage, error) {
	var all []json.RawMessage
	cursor := ""
	seen := map[string]bool{}
	for {
		query := `query($id: ID!, $cursor: String) { node(id: $id) { ... on ` + nodeType + ` { ` + field + `(first: 100, after: $cursor) { nodes { ` + fields + ` } pageInfo { hasNextPage endCursor } } } } }`
		vars := []string{"id=" + id}
		if cursor != "" {
			vars = append(vars, "cursor="+cursor)
		}
		data, err := c.graphql(ctx, "", host, query, vars...)
		if err != nil {
			return nil, err
		}
		var response struct{ Node map[string]json.RawMessage }
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, invalid("decode discussion: %v", err)
		}
		var connection struct {
			Nodes    []json.RawMessage
			PageInfo *struct {
				HasNextPage *bool
				EndCursor   string
			}
		}
		raw := response.Node[field]
		if len(raw) == 0 || string(raw) == "null" {
			return nil, invalid("missing %s connection", field)
		}
		if err := json.Unmarshal(raw, &connection); err != nil {
			return nil, invalid("decode %s: %v", field, err)
		}
		if connection.PageInfo == nil || connection.PageInfo.HasNextPage == nil || connection.Nodes == nil {
			return nil, invalid("missing %s pageInfo", field)
		}
		for _, node := range connection.Nodes {
			var identity struct{ ID string }
			if json.Unmarshal(node, &identity) != nil || identity.ID == "" {
				return nil, invalid("missing %s node identity", field)
			}
		}
		all = append(all, connection.Nodes...)
		if !*connection.PageInfo.HasNextPage {
			return all, nil
		}
		cursor = connection.PageInfo.EndCursor
		if cursor == "" || seen[cursor] {
			return nil, invalid("invalid %s pagination cursor", field)
		}
		seen[cursor] = true
	}
}
