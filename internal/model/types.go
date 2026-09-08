// Package model contains values shared by Glance's services and UI.
package model

import "time"

type EmptyReason string

const (
	NoWorkingPane EmptyReason = "no_working_pane"
	NoDirectory   EmptyReason = "no_directory"
	NotGit        EmptyReason = "not_git"
	DetachedHEAD  EmptyReason = "detached_head"
	UnbornHEAD    EmptyReason = "unborn_head"
)

type Source struct {
	WorkspaceID string
	TabID       string
	PaneID      string
	CWD         string
	Root        string
	Branch      string
	HEAD        string
	Visible     bool
	EmptyReason EmptyReason
}

const NoPR EmptyReason = "no_pr"

type PR struct {
	Host       string
	Repository string
	Number     int
	NodeID     string
	URL        string
}
type CheckState string

const (
	CheckFailed         CheckState = "failed"
	CheckTimedOut       CheckState = "timed_out"
	CheckCancelled      CheckState = "cancelled"
	CheckActionRequired CheckState = "action_required"
	CheckPending        CheckState = "pending"
	CheckPassed         CheckState = "passed"
	CheckNeutral        CheckState = "neutral"
	CheckSkipped        CheckState = "skipped"
	CheckUnknown        CheckState = "unknown"
)

type Check struct {
	Name, URL, Status, Conclusion string
	State                         CheckState
}
type CheckCounts struct{ Failed, Pending, Passed, Neutral, Skipped, Unknown int }
type Snapshot struct {
	PR                                          *PR
	EmptyReason                                 EmptyReason
	Title, Author, State                        string
	Draft                                       bool
	BaseBranch, HeadBranch, HeadRepository      string
	ReviewDecision                              string
	Commits, ChangedFiles, Additions, Deletions int
	Checks                                      []Check
	CheckCounts                                 CheckCounts
	FetchedAt                                   time.Time
}
type ErrorKind string

const (
	AuthenticationError  ErrorKind = "authentication"
	NetworkError         ErrorKind = "network"
	GitHubError          ErrorKind = "github"
	InvalidResponseError ErrorKind = "invalid_response"
)

type FetchError struct {
	Kind ErrorKind
	Err  error
}

func (e *FetchError) Error() string { return string(e.Kind) + ": " + e.Err.Error() }
func (e *FetchError) Unwrap() error { return e.Err }

// Section separates independently fetched and cached discussion payloads.
type Section string

const (
	Overview Section = "overview"
	Comments Section = "comments"
	Reviews  Section = "reviews"
)

type Comment struct {
	ID, Author, Body, URL string
	CreatedAt             time.Time
}
type Review struct {
	Comment
	State string
}
type ReviewThread struct {
	ID, Path, URL      string
	Line               *int
	Resolved, Outdated bool
	Comments           []Comment
}
type Discussion struct {
	PR        PR
	Section   Section
	Comments  []Comment
	Reviews   []Review
	Threads   []ReviewThread
	Complete  bool
	FetchedAt time.Time
}
type CacheEntry struct {
	SchemaVersion int
	PR            PR
	Section       Section
	FetchedAt     time.Time
	Data          Discussion
}

func (e CacheEntry) Fresh(now time.Time) bool { return now.Sub(e.FetchedAt) < 300*time.Second }
