// Package github retrieves PR data through the user's authenticated gh CLI.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cupsadarius/herdr-pr-glance/internal/command"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

type Runner interface {
	Run(context.Context, string, ...string) (string, error)
}
type Client struct {
	Runner Runner
	Now    func() time.Time
}

func (c Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
func (c Client) fetchError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	kind := model.GitHubError
	var e *command.Error
	if errors.As(err, &e) {
		s := strings.ToLower(e.Stderr)
		if e.ExitCode == 4 || strings.Contains(s, "authentication") || strings.Contains(s, "bad credentials") || strings.Contains(s, "gh auth login") {
			kind = model.AuthenticationError
		} else if strings.Contains(s, "error connecting") || strings.Contains(s, "dial tcp") || strings.Contains(s, "no such host") || strings.Contains(s, "connection refused") || strings.Contains(s, "tls handshake timeout") {
			kind = model.NetworkError
		}
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "rate limit") || strings.Contains(message, "rate_limit") || strings.Contains(message, "http 429") || strings.Contains(message, "too many requests") {
		kind = model.RateLimitError
	}
	var retry time.Time
	if kind == model.RateLimitError {
		if match := retryAfterHeader.FindStringSubmatch(err.Error()); len(match) > 1 {
			value := strings.TrimSpace(match[1])
			if seconds, e := strconv.ParseInt(value, 10, 64); e == nil && seconds >= 0 && seconds <= 86400*365 {
				retry = c.now().Add(time.Duration(seconds) * time.Second)
			} else if date, e := http.ParseTime(value); e == nil {
				retry = date
			}
		}
		if retry.IsZero() {
			if match := resetHeader.FindStringSubmatch(err.Error()); len(match) > 1 {
				if seconds, e := strconv.ParseInt(match[1], 10, 64); e == nil {
					retry = time.Unix(seconds, 0)
				}
			}
		}
	}
	return &model.FetchError{Kind: kind, Err: err, RetryAt: retry}
}

// gh does not always expose response headers; a zero RetryAt asks the UI to
// use its conservative five-minute fallback.
var retryAfterHeader = regexp.MustCompile(`(?im)retry-after:\s*([^\r\n]+)`)
var resetHeader = regexp.MustCompile(`(?im)x-ratelimit-reset:\s*(\d+)`)

func invalid(format string, args ...any) error {
	return &model.FetchError{Kind: model.InvalidResponseError, Err: fmt.Errorf(format, args...)}
}

// graphql rejects errors even if GitHub also returns usable-looking partial data.
func (c Client) graphql(ctx context.Context, cwd, host, query string, variables ...string) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	args := []string{"gh", "api", "graphql", "--hostname", host, "-f", "query=" + query}
	for _, v := range variables {
		args = append(args, "-f", v)
	}
	out, err := c.Runner.Run(ctx, cwd, args...)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	var envelope struct {
		Data   json.RawMessage
		Errors []struct{ Message string }
	}
	decodeErr := json.Unmarshal([]byte(out), &envelope)
	if decodeErr == nil && len(envelope.Errors) > 0 {
		messages := []string{}
		for _, e := range envelope.Errors {
			messages = append(messages, e.Message)
		}
		return nil, c.fetchError(fmt.Errorf("GraphQL: %s", strings.Join(messages, "; ")))
	}
	if err != nil {
		return nil, c.fetchError(err)
	}
	if decodeErr != nil {
		return nil, invalid("decode GraphQL: %v", decodeErr)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, invalid("missing GraphQL data")
	}
	return envelope.Data, nil
}
