package com.example.auth;

import com.example.annotations.Logged;
import com.example.annotations.Validated;

import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.security.SecureRandom;
import java.time.Duration;
import java.time.Instant;
import java.util.*;
import java.util.concurrent.ConcurrentHashMap;
import java.util.logging.Logger;

/**
 * Service for handling user authentication.
 *
 * This service manages user authentication, token generation,
 * and session management. It implements the Authenticator interface
 * and provides additional administrative functions.
 *
 * @see Authenticator
 */
public class AuthService implements Authenticator {

    private static final Logger logger = Logger.getLogger(AuthService.class.getName());
    private static final SecureRandom SECURE_RANDOM = new SecureRandom();

    /** Default token expiration time */
    public static final Duration DEFAULT_TOKEN_EXPIRY = Duration.ofHours(24);

    /** Maximum login attempts before lockout */
    public static final int MAX_LOGIN_ATTEMPTS = 5;

    private final Map<String, User> users = new ConcurrentHashMap<>();
    private final Map<String, String> passwords = new ConcurrentHashMap<>();
    private final Map<String, Token> tokens = new ConcurrentHashMap<>();
    private final Map<String, Integer> failedAttempts = new ConcurrentHashMap<>();

    private final Duration tokenExpiry;
    private final int maxAttempts;

    /**
     * Create an auth service with default configuration.
     */
    public AuthService() {
        this(DEFAULT_TOKEN_EXPIRY, MAX_LOGIN_ATTEMPTS);
    }

    /**
     * Create an auth service with custom configuration.
     *
     * @param tokenExpiry how long tokens are valid
     * @param maxAttempts maximum failed login attempts
     */
    public AuthService(Duration tokenExpiry, int maxAttempts) {
        this.tokenExpiry = tokenExpiry;
        this.maxAttempts = maxAttempts;
    }

    @Override
    @Logged
    @Validated
    public AuthResult authenticate(String email, String password) {
        Objects.requireNonNull(email, "email must not be null");
        Objects.requireNonNull(password, "password must not be null");

        // Check for account lockout
        if (isLocked(email)) {
            logger.warning("Locked account attempted login: " + maskEmail(email));
            return AuthResult.failure("Account is locked");
        }

        // Validate credentials
        User user = validateCredentials(email, password);
        if (user == null) {
            recordFailedAttempt(email);
            return AuthResult.failure("Invalid credentials");
        }

        // Generate tokens
        Token accessToken = generateToken(user.getId(), TokenType.ACCESS, List.of());
        Token refreshToken = generateToken(user.getId(), TokenType.REFRESH, List.of("refresh"));

        // Clear failed attempts on success
        failedAttempts.remove(email);

        logger.info("User authenticated: " + maskEmail(email));
        return AuthResult.success(user, accessToken, refreshToken);
    }

    @Override
    public AuthResult refresh(String refreshTokenValue) {
        Token token = tokens.get(refreshTokenValue);
        if (token == null) {
            return AuthResult.failure("Invalid refresh token");
        }

        if (token.isExpired()) {
            tokens.remove(refreshTokenValue);
            return AuthResult.failure("Refresh token has expired");
        }

        if (token.getType() != TokenType.REFRESH) {
            return AuthResult.failure("Not a refresh token");
        }

        User user = findUserById(token.getUserId());
        if (user == null) {
            return AuthResult.failure("User not found");
        }

        Token newAccessToken = generateToken(user.getId(), TokenType.ACCESS, token.getScopes());
        return AuthResult.success(user, newAccessToken, null);
    }

    @Override
    public Optional<User> validate(String accessToken) {
        Token token = tokens.get(accessToken);
        if (token == null || token.isExpired()) {
            return Optional.empty();
        }
        return Optional.ofNullable(findUserById(token.getUserId()));
    }

    @Override
    public boolean revoke(String tokenValue) {
        return tokens.remove(tokenValue) != null;
    }

    /**
     * Register a new user.
     *
     * @param email user's email address
     * @param password user's password
     * @param name user's display name
     * @param role user's role
     * @return the created user
     * @throws IllegalArgumentException if email is already registered
     */
    public User registerUser(String email, String password, String name, UserRole role) {
        if (users.containsKey(email)) {
            throw new IllegalArgumentException("Email already registered: " + email);
        }

        String userId = generateId();
        User user = new User(userId, email, name, role);

        users.put(email, user);
        passwords.put(email, hashPassword(password));

        logger.info("User registered: " + maskEmail(email));
        return user;
    }

    /**
     * Find a user by their ID.
     *
     * @param userId the user ID
     * @return the user or null
     */
    private User findUserById(String userId) {
        return users.values().stream()
                .filter(u -> u.getId().equals(userId))
                .findFirst()
                .orElse(null);
    }

    /**
     * Check if an account is locked due to too many failed attempts.
     */
    private boolean isLocked(String email) {
        return failedAttempts.getOrDefault(email, 0) >= maxAttempts;
    }

    /**
     * Record a failed login attempt.
     */
    private void recordFailedAttempt(String email) {
        int current = failedAttempts.getOrDefault(email, 0);
        failedAttempts.put(email, current + 1);
        logger.warning("Failed login attempt for " + maskEmail(email) + ": " + (current + 1));
    }

    /**
     * Validate email and password combination.
     */
    private User validateCredentials(String email, String password) {
        User user = users.get(email);
        if (user == null) {
            return null;
        }

        String storedHash = passwords.get(email);
        if (storedHash == null || !storedHash.equals(hashPassword(password))) {
            return null;
        }

        if (!user.isActive()) {
            return null;
        }

        return user;
    }

    /**
     * Generate a new authentication token.
     */
    private Token generateToken(String userId, TokenType type, List<String> scopes) {
        String value = generateSecureToken();
        Instant expiresAt = Instant.now().plus(tokenExpiry);

        Token token = new Token(value, type, userId, expiresAt, scopes);
        tokens.put(value, token);
        return token;
    }

    /**
     * Generate a cryptographically secure random token.
     */
    private static String generateSecureToken() {
        byte[] bytes = new byte[32];
        SECURE_RANDOM.nextBytes(bytes);
        return Base64.getUrlEncoder().withoutPadding().encodeToString(bytes);
    }

    /**
     * Generate a unique ID.
     */
    private static String generateId() {
        return UUID.randomUUID().toString();
    }

    /**
     * Hash a password using SHA-256.
     */
    private static String hashPassword(String password) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            byte[] hash = digest.digest(password.getBytes());
            return Base64.getEncoder().encodeToString(hash);
        } catch (NoSuchAlgorithmException e) {
            throw new RuntimeException("SHA-256 not available", e);
        }
    }

    /**
     * Mask an email address for logging.
     */
    private static String maskEmail(String email) {
        int atIndex = email.indexOf('@');
        if (atIndex <= 1) {
            return "***";
        }
        return email.charAt(0) + "***" + email.substring(atIndex);
    }
}
