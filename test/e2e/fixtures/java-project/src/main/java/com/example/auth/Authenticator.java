package com.example.auth;

import java.util.concurrent.CompletableFuture;

/**
 * Interface for authentication providers.
 *
 * Implementations can support various authentication mechanisms
 * such as username/password, OAuth, or API keys.
 */
public interface Authenticator {

    /**
     * Authenticate with email and password.
     *
     * @param email user's email address
     * @param password user's password
     * @return authentication result
     */
    AuthResult authenticate(String email, String password);

    /**
     * Authenticate asynchronously.
     *
     * @param email user's email address
     * @param password user's password
     * @return future containing authentication result
     */
    default CompletableFuture<AuthResult> authenticateAsync(String email, String password) {
        return CompletableFuture.supplyAsync(() -> authenticate(email, password));
    }

    /**
     * Refresh an access token using a refresh token.
     *
     * @param refreshToken the refresh token
     * @return authentication result with new tokens
     */
    AuthResult refresh(String refreshToken);

    /**
     * Validate an access token.
     *
     * @param accessToken the access token to validate
     * @return the user if valid, empty otherwise
     */
    java.util.Optional<User> validate(String accessToken);

    /**
     * Revoke a token (logout).
     *
     * @param token the token to revoke
     * @return true if successfully revoked
     */
    boolean revoke(String token);
}
