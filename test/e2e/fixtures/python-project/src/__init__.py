"""
Python Test Project

A comprehensive test fixture for verifying Python AST extraction.
Includes auth modules, data models, generics, and design patterns.
"""

__version__ = "1.0.0"
__author__ = "Test Project"

from .auth.service import AuthService
from .models.base import BaseEntity

__all__ = ["AuthService", "BaseEntity", "__version__"]
