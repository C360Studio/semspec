package com.example.auth;

/**
 * Types of authentication tokens.
 */
public enum TokenType {
    /**
     * Short-lived access token for API requests.
     */
    ACCESS("access"),

    /**
     * Long-lived refresh token for obtaining new access tokens.
     */
    REFRESH("refresh"),

    /**
     * API key for service-to-service authentication.
     */
    API_KEY("api_key");

    private final String value;

    TokenType(String value) {
        this.value = value;
    }

    /**
     * Get the string value of this token type.
     *
     * @return token type value
     */
    public String getValue() {
        return value;
    }
}
