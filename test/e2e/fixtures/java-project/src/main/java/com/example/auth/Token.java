package com.example.auth;

import java.time.Instant;
import java.util.Collections;
import java.util.List;
import java.util.Objects;

/**
 * Represents an authentication token.
 *
 * Tokens are immutable and used for authenticating API requests.
 * Each token has an expiration time and a set of permission scopes.
 */
public final class Token {

    private final String value;
    private final TokenType type;
    private final String userId;
    private final Instant expiresAt;
    private final List<String> scopes;

    /**
     * Creates a new token.
     *
     * @param value the token string
     * @param type the token type
     * @param userId the user this token belongs to
     * @param expiresAt when the token expires
     * @param scopes permission scopes
     */
    public Token(String value, TokenType type, String userId,
                 Instant expiresAt, List<String> scopes) {
        this.value = Objects.requireNonNull(value);
        this.type = Objects.requireNonNull(type);
        this.userId = Objects.requireNonNull(userId);
        this.expiresAt = Objects.requireNonNull(expiresAt);
        this.scopes = scopes != null ? List.copyOf(scopes) : Collections.emptyList();
    }

    /**
     * Check if this token has expired.
     *
     * @return true if expired
     */
    public boolean isExpired() {
        return Instant.now().isAfter(expiresAt);
    }

    /**
     * Check if this token has a specific scope.
     *
     * @param scope the scope to check
     * @return true if the token has the scope
     */
    public boolean hasScope(String scope) {
        return scopes.contains(scope) || scopes.contains("admin");
    }

    /**
     * Get the remaining lifetime in seconds.
     *
     * @return seconds until expiration, or 0 if expired
     */
    public long getRemainingSeconds() {
        long remaining = expiresAt.getEpochSecond() - Instant.now().getEpochSecond();
        return Math.max(0, remaining);
    }

    // Getters

    public String getValue() {
        return value;
    }

    public TokenType getType() {
        return type;
    }

    public String getUserId() {
        return userId;
    }

    public Instant getExpiresAt() {
        return expiresAt;
    }

    public List<String> getScopes() {
        return scopes;
    }

    @Override
    public boolean equals(Object o) {
        if (this == o) return true;
        if (o == null || getClass() != o.getClass()) return false;
        Token token = (Token) o;
        return Objects.equals(value, token.value);
    }

    @Override
    public int hashCode() {
        return Objects.hash(value);
    }
}
