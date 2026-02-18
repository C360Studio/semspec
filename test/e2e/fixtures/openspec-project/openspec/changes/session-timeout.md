---
title: Session Timeout Enhancement
modifies: auth.spec
---

# Session Timeout Changes

This delta spec proposes changes to the session timeout behavior.

## ADDED Requirements

### Requirement: Remember-Me

Allow users to opt for extended sessions on trusted devices.

The system MUST support a "remember me" option during login.
Remember-me sessions SHALL have a maximum duration of 30 days.
The system MUST allow users to revoke all remembered sessions.

#### Scenario: Remember Me Login

**GIVEN** a user checks the "remember me" option during login
**WHEN** they successfully authenticate
**THEN** their session is extended to 30 days instead of the default timeout

## MODIFIED Requirements

### Requirement: Session-Timeout

Updated timeout values for better user experience.

Sessions SHALL timeout after 60 minutes of inactivity (was 30 minutes).
The system MUST warn users 5 minutes before session expiration.
Expired sessions MUST require full re-authentication.

#### Scenario: Session Warning

**GIVEN** a user has an active session
**WHEN** there are 5 minutes remaining until timeout
**THEN** the user sees a warning notification with option to extend

## REMOVED Requirements

### Requirement: Forced-Logout

This requirement is being deprecated in favor of more user-friendly timeout warnings.

Forced logout was causing poor user experience during long form submissions.
