// Package github retrieves PR data through the user's authenticated gh CLI.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
func fetchError(err error) error {
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
	return &model.FetchError{Kind: kind, Err: err}
}
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
		return nil, fetchError(fmt.Errorf("GraphQL: %s", strings.Join(messages, "; ")))
	}
	if err != nil {
		return nil, fetchError(err)
	}
	if decodeErr != nil {
		return nil, invalid("decode GraphQL: %v", decodeErr)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, invalid("missing GraphQL data")
	}
	return envelope.Data, nil
}
