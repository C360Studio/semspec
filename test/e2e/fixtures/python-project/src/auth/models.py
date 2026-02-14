"""Data models for authentication."""

from dataclasses import dataclass, field
from datetime import datetime
from typing import Optional, List
from enum import Enum, auto


class UserRole(Enum):
    """User roles for access control."""
    ADMIN = auto()
    USER = auto()
    GUEST = auto()


class TokenType(Enum):
    """Types of authentication tokens."""
    ACCESS = "access"
    REFRESH = "refresh"
    API_KEY = "api_key"


@dataclass
class User:
    """Represents an authenticated user.

    Attributes:
        id: Unique user identifier
        email: User's email address
        name: User's display name
        role: User's role for authorization
        active: Whether the user account is active
        created_at: When the user was created
    """
    id: str
    email: str
    name: str
    role: UserRole = UserRole.USER
    active: bool = True
    created_at: datetime = field(default_factory=datetime.utcnow)

    def is_admin(self) -> bool:
        """Check if user has admin privileges."""
        return self.role == UserRole.ADMIN

    def can_access(self, resource: str) -> bool:
        """Check if user can access a resource."""
        if not self.active:
            return False
        if self.is_admin():
            return True
        # Guest users have limited access
        if self.role == UserRole.GUEST:
            return resource in ("public", "docs")
        return True


@dataclass
class Token:
    """Represents an authentication token.

    Attributes:
        value: The token string
        token_type: Type of token (access, refresh, etc.)
        user_id: ID of the user this token belongs to
        expires_at: When the token expires
        scopes: List of permission scopes
    """
    value: str
    token_type: TokenType
    user_id: str
    expires_at: datetime
    scopes: List[str] = field(default_factory=list)

    def is_expired(self) -> bool:
        """Check if the token has expired."""
        return datetime.utcnow() > self.expires_at

    def has_scope(self, scope: str) -> bool:
        """Check if the token has a specific scope."""
        return scope in self.scopes or "admin" in self.scopes


@dataclass
class AuthResult:
    """Result of an authentication attempt.

    Attributes:
        success: Whether authentication succeeded
        user: The authenticated user (if successful)
        access_token: Access token (if successful)
        refresh_token: Refresh token (if successful)
        error: Error message (if failed)
    """
    success: bool
    user: Optional[User] = None
    access_token: Optional[Token] = None
    refresh_token: Optional[Token] = None
    error: Optional[str] = None

    @classmethod
    def failure(cls, error: str) -> "AuthResult":
        """Create a failed authentication result."""
        return cls(success=False, error=error)

    @classmethod
    def ok(
        cls,
        user: User,
        access_token: Token,
        refresh_token: Optional[Token] = None
    ) -> "AuthResult":
        """Create a successful authentication result."""
        return cls(
            success=True,
            user=user,
            access_token=access_token,
            refresh_token=refresh_token
        )


# Module-level constants
DEFAULT_TOKEN_EXPIRY_HOURS = 24
MAX_LOGIN_ATTEMPTS = 5
MIN_PASSWORD_LENGTH = 8
