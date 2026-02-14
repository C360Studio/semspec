"""Authentication service implementation."""

import hashlib
import secrets
from datetime import datetime, timedelta
from typing import Optional, Dict, Callable
import logging

from .models import (
    User, Token, AuthResult, TokenType, UserRole,
    DEFAULT_TOKEN_EXPIRY_HOURS, MAX_LOGIN_ATTEMPTS
)


logger = logging.getLogger(__name__)


class AuthError(Exception):
    """Base exception for authentication errors."""
    pass


class InvalidCredentialsError(AuthError):
    """Raised when credentials are invalid."""
    pass


class AccountLockedError(AuthError):
    """Raised when account is locked due to too many failed attempts."""
    pass


class TokenExpiredError(AuthError):
    """Raised when a token has expired."""
    pass


class AuthService:
    """Service for handling user authentication.

    This service manages user authentication, token generation,
    and session management. It supports multiple authentication
    strategies and provides hooks for custom validation.

    Example:
        service = AuthService()
        result = await service.authenticate("user@example.com", "password")
        if result.success:
            print(f"Welcome, {result.user.name}")
    """

    def __init__(
        self,
        token_expiry_hours: int = DEFAULT_TOKEN_EXPIRY_HOURS,
        max_attempts: int = MAX_LOGIN_ATTEMPTS
    ):
        """Initialize the authentication service.

        Args:
            token_expiry_hours: Hours until tokens expire
            max_attempts: Maximum failed login attempts before lockout
        """
        self._users: Dict[str, User] = {}
        self._passwords: Dict[str, str] = {}
        self._tokens: Dict[str, Token] = {}
        self._failed_attempts: Dict[str, int] = {}
        self._token_expiry_hours = token_expiry_hours
        self._max_attempts = max_attempts
        self._validators: list[Callable[[str, str], bool]] = []

    def add_validator(self, validator: Callable[[str, str], bool]) -> None:
        """Add a custom credential validator.

        Args:
            validator: Function that takes email and password, returns bool
        """
        self._validators.append(validator)

    async def authenticate(
        self,
        email: str,
        password: str,
        scopes: Optional[list[str]] = None
    ) -> AuthResult:
        """Authenticate a user with email and password.

        Args:
            email: User's email address
            password: User's password
            scopes: Optional list of requested scopes

        Returns:
            AuthResult indicating success or failure

        Raises:
            AccountLockedError: If account is locked
        """
        # Check for account lockout
        if self._is_locked(email):
            logger.warning(f"Locked account attempted login: {email}")
            raise AccountLockedError(f"Account {email} is locked")

        # Validate credentials
        user = await self._validate_credentials(email, password)
        if user is None:
            self._record_failed_attempt(email)
            return AuthResult.failure("Invalid credentials")

        # Run custom validators
        for validator in self._validators:
            if not validator(email, password):
                return AuthResult.failure("Validation failed")

        # Generate tokens
        access_token = self._generate_token(user.id, TokenType.ACCESS, scopes or [])
        refresh_token = self._generate_token(user.id, TokenType.REFRESH, ["refresh"])

        # Clear failed attempts on success
        self._failed_attempts.pop(email, None)

        logger.info(f"User authenticated: {email}")
        return AuthResult.ok(user, access_token, refresh_token)

    async def refresh_token(self, refresh_token_value: str) -> AuthResult:
        """Refresh an access token using a refresh token.

        Args:
            refresh_token_value: The refresh token string

        Returns:
            AuthResult with new tokens
        """
        token = self._tokens.get(refresh_token_value)
        if token is None:
            return AuthResult.failure("Invalid refresh token")

        if token.is_expired():
            del self._tokens[refresh_token_value]
            raise TokenExpiredError("Refresh token has expired")

        if token.token_type != TokenType.REFRESH:
            return AuthResult.failure("Not a refresh token")

        user = self._users.get(token.user_id)
        if user is None:
            return AuthResult.failure("User not found")

        # Generate new access token
        new_access = self._generate_token(user.id, TokenType.ACCESS, token.scopes)

        return AuthResult.ok(user, new_access)

    async def validate_token(self, token_value: str) -> Optional[User]:
        """Validate an access token and return the user.

        Args:
            token_value: The access token string

        Returns:
            The User if token is valid, None otherwise
        """
        token = self._tokens.get(token_value)
        if token is None or token.is_expired():
            return None

        return self._users.get(token.user_id)

    async def logout(self, token_value: str) -> bool:
        """Invalidate a token (logout).

        Args:
            token_value: The token to invalidate

        Returns:
            True if token was invalidated, False if not found
        """
        if token_value in self._tokens:
            del self._tokens[token_value]
            return True
        return False

    def register_user(
        self,
        email: str,
        password: str,
        name: str,
        role: UserRole = UserRole.USER
    ) -> User:
        """Register a new user.

        Args:
            email: User's email address
            password: User's password (will be hashed)
            name: User's display name
            role: User's role

        Returns:
            The created User

        Raises:
            ValueError: If email is already registered
        """
        if email in self._users:
            raise ValueError(f"Email already registered: {email}")

        user_id = secrets.token_hex(16)
        user = User(
            id=user_id,
            email=email,
            name=name,
            role=role
        )

        self._users[email] = user
        self._passwords[email] = self._hash_password(password)

        logger.info(f"User registered: {email}")
        return user

    def _is_locked(self, email: str) -> bool:
        """Check if an account is locked."""
        return self._failed_attempts.get(email, 0) >= self._max_attempts

    def _record_failed_attempt(self, email: str) -> None:
        """Record a failed login attempt."""
        current = self._failed_attempts.get(email, 0)
        self._failed_attempts[email] = current + 1
        logger.warning(f"Failed login attempt for {email}: {current + 1}")

    async def _validate_credentials(
        self,
        email: str,
        password: str
    ) -> Optional[User]:
        """Validate email and password combination."""
        user = self._users.get(email)
        if user is None:
            return None

        stored_hash = self._passwords.get(email)
        if stored_hash is None:
            return None

        if self._hash_password(password) != stored_hash:
            return None

        if not user.active:
            return None

        return user

    def _generate_token(
        self,
        user_id: str,
        token_type: TokenType,
        scopes: list[str]
    ) -> Token:
        """Generate a new authentication token."""
        value = secrets.token_urlsafe(32)
        expires_at = datetime.utcnow() + timedelta(hours=self._token_expiry_hours)

        token = Token(
            value=value,
            token_type=token_type,
            user_id=user_id,
            expires_at=expires_at,
            scopes=scopes
        )

        self._tokens[value] = token
        return token

    @staticmethod
    def _hash_password(password: str) -> str:
        """Hash a password for secure storage."""
        return hashlib.sha256(password.encode()).hexdigest()
