package com.example.auth;

import java.time.Instant;
import java.util.Objects;

/**
 * Represents an authenticated user in the system.
 *
 * This class holds core user identity information and provides
 * methods for checking user permissions and status.
 */
public class User {

    private final String id;
    private final String email;
    private final String name;
    private UserRole role;
    private boolean active;
    private final Instant createdAt;

    /**
     * Creates a new user with the specified details.
     *
     * @param id unique user identifier
     * @param email user's email address
     * @param name user's display name
     * @param role user's authorization role
     */
    public User(String id, String email, String name, UserRole role) {
        this.id = Objects.requireNonNull(id, "id must not be null");
        this.email = Objects.requireNonNull(email, "email must not be null");
        this.name = Objects.requireNonNull(name, "name must not be null");
        this.role = Objects.requireNonNull(role, "role must not be null");
        this.active = true;
        this.createdAt = Instant.now();
    }

    /**
     * Check if the user has admin privileges.
     *
     * @return true if user is an admin
     */
    public boolean isAdmin() {
        return role == UserRole.ADMIN;
    }

    /**
     * Check if the user can access a specific resource.
     *
     * @param resource the resource identifier
     * @return true if access is allowed
     */
    public boolean canAccess(String resource) {
        if (!active) {
            return false;
        }
        if (isAdmin()) {
            return true;
        }
        if (role == UserRole.GUEST) {
            return "public".equals(resource) || "docs".equals(resource);
        }
        return true;
    }

    /**
     * Deactivate this user account.
     */
    public void deactivate() {
        this.active = false;
    }

    /**
     * Reactivate this user account.
     */
    public void activate() {
        this.active = true;
    }

    // Getters

    public String getId() {
        return id;
    }

    public String getEmail() {
        return email;
    }

    public String getName() {
        return name;
    }

    public UserRole getRole() {
        return role;
    }

    public void setRole(UserRole role) {
        this.role = Objects.requireNonNull(role);
    }

    public boolean isActive() {
        return active;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }

    @Override
    public boolean equals(Object o) {
        if (this == o) return true;
        if (o == null || getClass() != o.getClass()) return false;
        User user = (User) o;
        return Objects.equals(id, user.id);
    }

    @Override
    public int hashCode() {
        return Objects.hash(id);
    }

    @Override
    public String toString() {
        return "User{" +
                "id='" + id + '\'' +
                ", email='" + email + '\'' +
                ", name='" + name + '\'' +
                ", role=" + role +
                ", active=" + active +
                '}';
    }
}
