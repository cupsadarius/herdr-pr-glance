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

// StackEntry is one pull request of a stack, as GitHub reports it. Position 1
// is the bottom of the stack, closest to the trunk.
type StackEntry struct {
	Position                             int
	PR                                   PR
	Title, State, HeadBranch, BaseBranch string
	ReviewDecision                       string
	Draft                                bool
}

// Stack is a GitHub pull request stack. Entries are sorted by Position
// ascending and hold at most the first page GitHub returns, so Size can
// exceed len(Entries).
type Stack struct {
	Number, Size int
	BaseBranch   string
	Entries      []StackEntry
}
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
	// Stack is nil unless the pull request belongs to a stack; StackPosition
	// is then its position within it, and zero otherwise.
	Stack         *Stack
	StackPosition int
	FetchedAt     time.Time
}
type ErrorKind string

const (
	RateLimitError       ErrorKind = "rate_limit"
	AuthenticationError  ErrorKind = "authentication"
	NetworkError         ErrorKind = "network"
	GitHubError          ErrorKind = "github"
	InvalidResponseError ErrorKind = "invalid_response"
)

type FetchError struct {
	// RetryAt is the known rate-limit retry time; zero requests a five-minute fallback.
	RetryAt time.Time
	Kind    ErrorKind
	Err     error
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

// DiscussionFreshness is the single cache-freshness policy, shared by the disk
// cache and by the UI's staleness check.
const DiscussionFreshness = 300 * time.Second

func (e CacheEntry) Fresh(now time.Time) bool { return now.Sub(e.FetchedAt) < DiscussionFreshness }
