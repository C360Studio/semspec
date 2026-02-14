package com.example.auth;

/**
 * User roles for access control and authorization.
 */
public enum UserRole {
    /**
     * Administrator with full system access.
     */
    ADMIN("admin", 100),

    /**
     * Regular authenticated user.
     */
    USER("user", 50),

    /**
     * Guest user with limited access.
     */
    GUEST("guest", 10);

    private final String code;
    private final int level;

    UserRole(String code, int level) {
        this.code = code;
        this.level = level;
    }

    /**
     * Get the string code for this role.
     *
     * @return role code
     */
    public String getCode() {
        return code;
    }

    /**
     * Get the privilege level for this role.
     *
     * @return privilege level (higher is more privileged)
     */
    public int getLevel() {
        return level;
    }

    /**
     * Check if this role has at least the given privilege level.
     *
     * @param requiredLevel the minimum required level
     * @return true if this role meets or exceeds the level
     */
    public boolean hasLevel(int requiredLevel) {
        return this.level >= requiredLevel;
    }

    /**
     * Parse a role from its code string.
     *
     * @param code the role code
     * @return the matching role
     * @throws IllegalArgumentException if no role matches
     */
    public static UserRole fromCode(String code) {
        for (UserRole role : values()) {
            if (role.code.equals(code)) {
                return role;
            }
        }
        throw new IllegalArgumentException("Unknown role code: " + code);
    }
}
