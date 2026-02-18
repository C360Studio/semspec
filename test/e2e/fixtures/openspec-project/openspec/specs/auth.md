---
title: Authentication Specification
applies_to:
  - "auth/**/*.go"
  - "session/*.go"
---

# Authentication

This specification defines the authentication requirements for the system.

### Requirement: Token-Refresh

The token refresh mechanism ensures users maintain authenticated sessions without manual re-authentication.

The system MUST refresh access tokens before expiration.
The system SHALL NOT accept expired tokens for any authenticated operation.
The refresh token MUST be stored securely using encryption at rest.

Applies to: `auth/token.go`, `auth/refresh.go`

#### Scenario: Valid Token Refresh

**GIVEN** a user has a valid refresh token that has not expired
**WHEN** the access token is within 5 minutes of expiration
**THEN** a new access token is issued automatically

#### Scenario: Expired Refresh Token

**GIVEN** a user has an expired refresh token
**WHEN** they attempt to refresh the access token
**THEN** the request is rejected with a 401 Unauthorized status

### Requirement: Session-Timeout

User sessions must have appropriate timeout controls for security.

Sessions SHALL timeout after 30 minutes of inactivity.
The system MUST provide a session extension mechanism.
Expired sessions MUST require full re-authentication.

#### Scenario: Idle Session Timeout

**GIVEN** a user has an active session
**WHEN** there is no activity for 30 minutes
**THEN** the session is invalidated and the user must re-authenticate

### Requirement: MFA-Optional

Multi-factor authentication should be available but optional.

The system MUST support TOTP-based multi-factor authentication.
Users SHALL be able to enable or disable MFA from their settings.
MFA enrollment SHALL require verification of the second factor.

#### Scenario: MFA Enrollment

**GIVEN** a user without MFA enabled
**WHEN** they choose to enable MFA in settings
**THEN** they are prompted to scan a QR code and verify a TOTP code
