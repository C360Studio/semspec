package com.example.auth;

import java.util.Optional;

/**
 * Result of an authentication attempt.
 *
 * Uses the Result pattern to encapsulate either a successful
 * authentication or an error message.
 */
public record AuthResult(
        boolean success,
        User user,
        Token accessToken,
        Token refreshToken,
        String error
) {
    /**
     * Create a successful authentication result.
     *
     * @param user the authenticated user
     * @param accessToken the access token
     * @param refreshToken the refresh token (optional)
     * @return successful result
     */
    public static AuthResult success(User user, Token accessToken, Token refreshToken) {
        return new AuthResult(true, user, accessToken, refreshToken, null);
    }

    /**
     * Create a failed authentication result.
     *
     * @param error the error message
     * @return failed result
     */
    public static AuthResult failure(String error) {
        return new AuthResult(false, null, null, null, error);
    }

    /**
     * Get the user if authentication succeeded.
     *
     * @return optional user
     */
    public Optional<User> getUser() {
        return Optional.ofNullable(user);
    }

    /**
     * Get the access token if authentication succeeded.
     *
     * @return optional access token
     */
    public Optional<Token> getAccessToken() {
        return Optional.ofNullable(accessToken);
    }

    /**
     * Get the refresh token if present.
     *
     * @return optional refresh token
     */
    public Optional<Token> getRefreshToken() {
        return Optional.ofNullable(refreshToken);
    }

    /**
     * Get the error message if authentication failed.
     *
     * @return optional error message
     */
    public Optional<String> getError() {
        return Optional.ofNullable(error);
    }
}
