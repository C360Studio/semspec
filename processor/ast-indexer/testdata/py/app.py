"""Application module for user management."""

from dataclasses import dataclass
from typing import Optional


@dataclass
class User:
    """Represents a user in the system."""
    id: int
    name: str
    email: str


def create_user(name: str, email: str) -> User:
    """Create a new user with the given details."""
    return User(id=0, name=name, email=email)


class UserService:
    """Service for managing users."""

    def __init__(self):
        self.users: list[User] = []

    def add_user(self, user: User) -> None:
        """Add a user to the service."""
        self.users.append(user)

    def get_user(self, user_id: int) -> Optional[User]:
        """Get a user by ID."""
        for user in self.users:
            if user.id == user_id:
                return user
        return None
