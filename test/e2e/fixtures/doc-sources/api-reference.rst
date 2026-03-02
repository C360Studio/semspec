:category: api-reference
:scope: code

==============
API Reference
==============

This document provides the API reference for the authentication service.

Authentication
==============

The authentication API provides endpoints for user login and token management.

Login Endpoint
--------------

POST /api/v1/login

Request body::

    {
        "username": "user@example.com",
        "password": "secret"
    }

Response::

    {
        "token": "eyJhbGciOiJIUzI1NiIs...",
        "expires_at": "2024-01-01T00:00:00Z"
    }

Token Refresh
-------------

POST /api/v1/refresh

Refreshes an expired or soon-to-expire token.

.. note::

    Tokens must be refreshed before they expire to maintain session continuity.

Error Codes
===========

The API returns standard HTTP status codes:

* 200 - Success
* 401 - Unauthorized
* 403 - Forbidden
* 500 - Internal Server Error
