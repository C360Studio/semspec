## ADDED Requirements

### Requirement: Git status tool
The system SHALL provide a `git_status` tool for agents to check repository state.

#### Scenario: Check clean status
- **WHEN** agent calls `git_status` on repo with no changes
- **THEN** tool returns status indicating working tree is clean

#### Scenario: Check dirty status
- **WHEN** agent calls `git_status` on repo with uncommitted changes
- **THEN** tool returns list of modified, staged, and untracked files

#### Scenario: Status on non-git directory
- **WHEN** agent calls `git_status` on directory without git repo
- **THEN** tool returns error indicating not a git repository

### Requirement: Git branch tool
The system SHALL provide a `git_branch` tool for agents to manage branches.

#### Scenario: Create new branch
- **WHEN** agent calls `git_branch` with new branch name
- **THEN** tool creates branch from current HEAD
- **AND** tool switches to the new branch

#### Scenario: Create branch with base
- **WHEN** agent calls `git_branch` with branch name and base ref
- **THEN** tool creates branch from specified base
- **AND** tool switches to the new branch

#### Scenario: Switch to existing branch
- **WHEN** agent calls `git_branch` with existing branch name
- **THEN** tool switches to the specified branch

#### Scenario: Branch with uncommitted changes
- **WHEN** agent calls `git_branch` with uncommitted changes that conflict
- **THEN** tool returns error indicating uncommitted changes would be overwritten

### Requirement: Git commit tool
The system SHALL provide a `git_commit` tool for agents to commit changes.

#### Scenario: Commit staged changes
- **WHEN** agent calls `git_commit` with message
- **THEN** tool commits all staged changes with the message
- **AND** tool returns the commit hash

#### Scenario: Commit with auto-stage
- **WHEN** agent calls `git_commit` with message and `stage_all: true`
- **THEN** tool stages all modified tracked files
- **AND** tool commits with the message

#### Scenario: Commit with no changes
- **WHEN** agent calls `git_commit` with no staged changes
- **THEN** tool returns error indicating nothing to commit

#### Scenario: Commit message format
- **WHEN** agent calls `git_commit`
- **THEN** tool validates message follows conventional commit format
- **AND** tool proceeds with commit if valid

### Requirement: Git operation safety
All git tools SHALL operate only within the configured repository.

#### Scenario: Reject operation outside repo
- **WHEN** agent attempts git operation on path outside configured repo
- **THEN** tool returns error indicating access denied
