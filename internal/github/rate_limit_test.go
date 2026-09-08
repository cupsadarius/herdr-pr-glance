package github

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cupsadarius/herdr-pr-glance/internal/command"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

func TestRateLimitClassification(t *testing.T) {
	now := time.Unix(1000, 0)
	c := Client{Now: func() time.Time { return now }}
	for _, tt := range []struct {
		message string
		retry   time.Time
	}{
		{"gh: API rate limit exceeded for user", time.Time{}},
		{"gh: You have exceeded a secondary rate limit. Retry-After: 90", now.Add(90 * time.Second)},
		{"HTTP 429: too many requests\nX-RateLimit-Reset: 2000", time.Unix(2000, 0)},
		{"GraphQL: API rate limit exceeded", time.Time{}},
	} {
		t.Run(tt.message, func(t *testing.T) {
			err := c.fetchError(&command.Error{Stderr: tt.message, ExitCode: 1})
			var got *model.FetchError
			if !errors.As(err, &got) || got.Kind != model.RateLimitError || !got.RetryAt.Equal(tt.retry) {
				t.Fatalf("got %#v", got)
			}
		})
	}
}

func TestSnapshotAndGraphQLExposeRateLimits(t *testing.T) {
	c := Client{Runner: &fixtureRunner{t: t, replies: []reply{{err: &command.Error{ExitCode: 1, Stderr: "gh: API rate limit exceeded for user"}}}}}
	_, err := c.Snapshot(context.Background(), model.Source{CWD: "/source", Branch: "feature"})
	var fetch *model.FetchError
	if !errors.As(err, &fetch) || fetch.Kind != model.RateLimitError {
		t.Fatalf("snapshot error = %v", err)
	}
	c.Runner = &fixtureRunner{t: t, replies: []reply{{out: `{"errors":[{"message":"API rate limit exceeded","type":"RATE_LIMITED"}]}`}}}
	_, err = c.graphql(context.Background(), "/source", "github.com", "query{}")
	if !errors.As(err, &fetch) || fetch.Kind != model.RateLimitError {
		t.Fatalf("GraphQL error = %v", err)
	}
}
