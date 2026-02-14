"""Utility functions for authentication."""

import re
import secrets
from typing import Optional, Tuple
from functools import lru_cache


# Password validation constants
PASSWORD_MIN_LENGTH = 8
PASSWORD_MAX_LENGTH = 128
PASSWORD_REQUIRE_UPPERCASE = True
PASSWORD_REQUIRE_LOWERCASE = True
PASSWORD_REQUIRE_DIGIT = True
PASSWORD_REQUIRE_SPECIAL = True


def validate_email(email: str) -> Tuple[bool, Optional[str]]:
    """Validate an email address format.

    Args:
        email: The email address to validate

    Returns:
        Tuple of (is_valid, error_message)
    """
    if not email:
        return False, "Email is required"

    # Basic email pattern
    pattern = r'^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$'
    if not re.match(pattern, email):
        return False, "Invalid email format"

    if len(email) > 254:
        return False, "Email too long"

    return True, None


def validate_password(password: str) -> Tuple[bool, Optional[str]]:
    """Validate a password meets security requirements.

    Args:
        password: The password to validate

    Returns:
        Tuple of (is_valid, error_message)
    """
    if not password:
        return False, "Password is required"

    if len(password) < PASSWORD_MIN_LENGTH:
        return False, f"Password must be at least {PASSWORD_MIN_LENGTH} characters"

    if len(password) > PASSWORD_MAX_LENGTH:
        return False, f"Password must be at most {PASSWORD_MAX_LENGTH} characters"

    if PASSWORD_REQUIRE_UPPERCASE and not re.search(r'[A-Z]', password):
        return False, "Password must contain uppercase letter"

    if PASSWORD_REQUIRE_LOWERCASE and not re.search(r'[a-z]', password):
        return False, "Password must contain lowercase letter"

    if PASSWORD_REQUIRE_DIGIT and not re.search(r'\d', password):
        return False, "Password must contain a digit"

    if PASSWORD_REQUIRE_SPECIAL and not re.search(r'[!@#$%^&*(),.?":{}|<>]', password):
        return False, "Password must contain a special character"

    return True, None


def generate_secure_token(length: int = 32) -> str:
    """Generate a cryptographically secure random token.

    Args:
        length: Length of the token in bytes (output will be hex encoded)

    Returns:
        A hex-encoded secure random string
    """
    return secrets.token_hex(length)


def generate_api_key() -> str:
    """Generate a formatted API key.

    Returns:
        An API key in the format 'sk_xxxx_xxxx_xxxx_xxxx'
    """
    parts = [secrets.token_hex(4) for _ in range(4)]
    return f"sk_{'_'.join(parts)}"


@lru_cache(maxsize=1024)
def normalize_email(email: str) -> str:
    """Normalize an email address for comparison.

    Handles case insensitivity and common email aliasing patterns.

    Args:
        email: The email address to normalize

    Returns:
        The normalized email address
    """
    email = email.lower().strip()

    # Split local and domain parts
    if '@' not in email:
        return email

    local, domain = email.rsplit('@', 1)

    # Handle Gmail-style plus addressing
    if '+' in local:
        local = local.split('+')[0]

    # Remove dots from Gmail addresses
    if domain in ('gmail.com', 'googlemail.com'):
        local = local.replace('.', '')

    return f"{local}@{domain}"


def mask_email(email: str) -> str:
    """Mask an email address for display (e.g., in logs).

    Args:
        email: The email address to mask

    Returns:
        Masked email (e.g., "j***@example.com")
    """
    if '@' not in email:
        return '***'

    local, domain = email.rsplit('@', 1)
    if len(local) <= 1:
        return f"*@{domain}"

    return f"{local[0]}***@{domain}"


def constant_time_compare(a: str, b: str) -> bool:
    """Compare two strings in constant time to prevent timing attacks.

    Args:
        a: First string
        b: Second string

    Returns:
        True if strings are equal, False otherwise
    """
    return secrets.compare_digest(a.encode(), b.encode())
