// Package model contains values shared by Glance's services and UI.
package model

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
