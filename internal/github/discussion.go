package github

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

const commentFields = `id body url createdAt author { login }`
const reviewFields = `id body url submittedAt state author { login }`
const threadFields = `id path line isResolved isOutdated`

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
		nodes, err := c.discussionPages(ctx, pr.Host, pr.NodeID, "PullRequest", "comments", commentFields, "")
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
		if err := c.reviewSection(ctx, &d, pr.Host, pr.NodeID); err != nil {
			return d, err
		}
	default:
		return d, invalid("unsupported discussion section %q", section)
	}
	d.Complete = true
	d.FetchedAt = c.now()
	return d, nil
}

// replyOverflow points at a thread whose first reply page did not exhaust its connection.
type replyOverflow struct {
	id, cursor string
	thread     int
}

// reviewSection batches reviews, review threads and each thread's first page of
// replies into one query. GraphQL points are charged per connection request, not
// per node, so nesting comments(first: 100) under reviewThreads(first: 100) costs
// the same single request that the old code paid once per thread (N+1 subprocesses).
// Only a connection that reports hasNextPage earns a follow-up request.
func (c Client) reviewSection(ctx context.Context, d *model.Discussion, host, id string) error {
	reviews, threads := newCursor(""), newCursor("")
	var overflow []replyOverflow
	for !reviews.done || !threads.done {
		query, vars := reviewQuery(id, reviews, threads)
		data, err := c.graphql(ctx, "", host, query, vars...)
		if err != nil {
			return err
		}
		var response struct{ Node map[string]json.RawMessage }
		if err := json.Unmarshal(data, &response); err != nil {
			return invalid("decode discussion: %v", err)
		}
		if !reviews.done {
			page, err := decodeConnection(response.Node["reviews"], "reviews")
			if err != nil {
				return err
			}
			if err := appendReviews(d, page.Nodes); err != nil {
				return err
			}
			if err := reviews.advance("reviews", page); err != nil {
				return err
			}
		}
		if !threads.done {
			page, err := decodeConnection(response.Node["reviewThreads"], "reviewThreads")
			if err != nil {
				return err
			}
			more, err := appendThreads(d, page.Nodes)
			if err != nil {
				return err
			}
			overflow = append(overflow, more...)
			if err := threads.advance("reviewThreads", page); err != nil {
				return err
			}
		}
	}
	for _, o := range overflow {
		replies, err := c.discussionPages(ctx, host, o.id, "PullRequestReviewThread", "comments", commentFields, o.cursor)
		if err != nil {
			return err
		}
		if err := appendReplies(&d.Threads[o.thread], replies); err != nil {
			return err
		}
	}
	sort.SliceStable(d.Threads, func(i, j int) bool { return !d.Threads[i].Resolved && d.Threads[j].Resolved })
	return nil
}

// reviewQuery asks only for connections that still have pages left. An exhausted
// connection drops both its selection and its cursor variable, since GraphQL
// rejects an operation that declares a variable it never uses.
func reviewQuery(id string, reviews, threads cursor) (string, []string) {
	fields, declarations, vars := "", "$id: ID!", []string{"id=" + id}
	if !reviews.done {
		declarations += ", $reviewCursor: String"
		fields += ` reviews(first: 100, after: $reviewCursor) { nodes { ` + reviewFields + ` } pageInfo { hasNextPage endCursor } }`
		if reviews.after != "" {
			vars = append(vars, "reviewCursor="+reviews.after)
		}
	}
	if !threads.done {
		declarations += ", $threadCursor: String"
		fields += ` reviewThreads(first: 100, after: $threadCursor) { nodes { ` + threadFields +
			` comments(first: 100) { nodes { ` + commentFields + ` } pageInfo { hasNextPage endCursor } } }` +
			` pageInfo { hasNextPage endCursor } }`
		if threads.after != "" {
			vars = append(vars, "threadCursor="+threads.after)
		}
	}
	return `query(` + declarations + `) { node(id: $id) { ... on PullRequest {` + fields + ` } } }`, vars
}

func appendReviews(d *model.Discussion, nodes []json.RawMessage) error {
	for _, raw := range nodes {
		var v discussionComment
		if err := json.Unmarshal(raw, &v); err != nil {
			return invalid("decode review: %v", err)
		}
		if v.State == "PENDING" {
			continue
		}
		v.CreatedAt = v.SubmittedAt
		d.Reviews = append(d.Reviews, model.Review{Comment: v.comment(), State: v.State})
	}
	return nil
}

func appendThreads(d *model.Discussion, nodes []json.RawMessage) ([]replyOverflow, error) {
	var overflow []replyOverflow
	for _, raw := range nodes {
		var v struct {
			ID, Path               string
			Line                   *int
			IsResolved, IsOutdated bool
			Comments               json.RawMessage
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, invalid("decode thread: %v", err)
		}
		replies, err := decodeConnection(v.Comments, "comments")
		if err != nil {
			return nil, err
		}
		thread := model.ReviewThread{ID: v.ID, Path: v.Path, Line: v.Line, Resolved: v.IsResolved, Outdated: v.IsOutdated}
		if err := appendReplies(&thread, replies.Nodes); err != nil {
			return nil, err
		}
		if *replies.PageInfo.HasNextPage {
			if replies.PageInfo.EndCursor == "" {
				return nil, invalid("invalid comments pagination cursor")
			}
			overflow = append(overflow, replyOverflow{id: v.ID, cursor: replies.PageInfo.EndCursor, thread: len(d.Threads)})
		}
		d.Threads = append(d.Threads, thread)
	}
	return overflow, nil
}

func appendReplies(thread *model.ReviewThread, nodes []json.RawMessage) error {
	for _, raw := range nodes {
		var comment discussionComment
		if err := json.Unmarshal(raw, &comment); err != nil {
			return invalid("decode reply: %v", err)
		}
		thread.Comments = append(thread.Comments, comment.comment())
	}
	if len(thread.Comments) > 0 {
		thread.URL = thread.Comments[0].URL
	}
	return nil
}

// connection is one page of a GraphQL connection; every page must carry pageInfo
// and identifiable nodes before any of it is trusted.
type connection struct {
	Nodes    []json.RawMessage
	PageInfo *struct {
		HasNextPage *bool
		EndCursor   string
	}
}

func decodeConnection(raw json.RawMessage, field string) (connection, error) {
	var page connection
	if len(raw) == 0 || string(raw) == "null" {
		return page, invalid("missing %s connection", field)
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return page, invalid("decode %s: %v", field, err)
	}
	if page.PageInfo == nil || page.PageInfo.HasNextPage == nil || page.Nodes == nil {
		return page, invalid("missing %s pageInfo", field)
	}
	for _, node := range page.Nodes {
		var identity struct{ ID string }
		if json.Unmarshal(node, &identity) != nil || identity.ID == "" {
			return page, invalid("missing %s node identity", field)
		}
	}
	return page, nil
}

// cursor tracks one connection's pagination; each connection owns its own,
// including every thread's reply connection.
type cursor struct {
	after string
	seen  map[string]bool
	done  bool
}

func newCursor(after string) cursor {
	seen := map[string]bool{}
	if after != "" {
		seen[after] = true
	}
	return cursor{after: after, seen: seen}
}

// advance rejects an empty or repeated cursor rather than looping or truncating.
func (c *cursor) advance(field string, page connection) error {
	if !*page.PageInfo.HasNextPage {
		c.done = true
		return nil
	}
	next := page.PageInfo.EndCursor
	if next == "" || c.seen[next] {
		return invalid("invalid %s pagination cursor", field)
	}
	c.seen[next] = true
	c.after = next
	return nil
}

func (c Client) discussionPages(ctx context.Context, host, id, nodeType, field, fields, after string) ([]json.RawMessage, error) {
	var all []json.RawMessage
	page := newCursor(after)
	for !page.done {
		query := `query($id: ID!, $cursor: String) { node(id: $id) { ... on ` + nodeType + ` { ` + field +
			`(first: 100, after: $cursor) { nodes { ` + fields + ` } pageInfo { hasNextPage endCursor } } } } }`
		vars := []string{"id=" + id}
		if page.after != "" {
			vars = append(vars, "cursor="+page.after)
		}
		data, err := c.graphql(ctx, "", host, query, vars...)
		if err != nil {
			return nil, err
		}
		var response struct{ Node map[string]json.RawMessage }
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, invalid("decode discussion: %v", err)
		}
		connection, err := decodeConnection(response.Node[field], field)
		if err != nil {
			return nil, err
		}
		all = append(all, connection.Nodes...)
		if err := page.advance(field, connection); err != nil {
			return nil, err
		}
	}
	return all, nil
}
