package com.example.models;

/**
 * Status enum representing the lifecycle state of an entity.
 *
 * Includes metadata about each status and transition rules.
 */
public enum Status {

    /**
     * Initial state when entity is created but not yet active.
     */
    DRAFT("draft", "Draft", false, true),

    /**
     * Entity is pending review or approval.
     */
    PENDING("pending", "Pending", false, true),

    /**
     * Entity is active and in use.
     */
    ACTIVE("active", "Active", true, true),

    /**
     * Entity is temporarily disabled.
     */
    SUSPENDED("suspended", "Suspended", false, true),

    /**
     * Entity has been archived (soft delete).
     */
    ARCHIVED("archived", "Archived", false, false),

    /**
     * Entity has been permanently deleted.
     */
    DELETED("deleted", "Deleted", false, false);

    private final String code;
    private final String displayName;
    private final boolean active;
    private final boolean editable;

    Status(String code, String displayName, boolean active, boolean editable) {
        this.code = code;
        this.displayName = displayName;
        this.active = active;
        this.editable = editable;
    }

    /**
     * Get the status code for persistence.
     *
     * @return status code
     */
    public String getCode() {
        return code;
    }

    /**
     * Get the human-readable display name.
     *
     * @return display name
     */
    public String getDisplayName() {
        return displayName;
    }

    /**
     * Check if entities in this status are considered active.
     *
     * @return true if active
     */
    public boolean isActive() {
        return active;
    }

    /**
     * Check if entities in this status can be edited.
     *
     * @return true if editable
     */
    public boolean isEditable() {
        return editable;
    }

    /**
     * Check if this status can transition to the target status.
     *
     * @param target the target status
     * @return true if transition is allowed
     */
    public boolean canTransitionTo(Status target) {
        return switch (this) {
            case DRAFT -> target == PENDING || target == ACTIVE || target == DELETED;
            case PENDING -> target == ACTIVE || target == DRAFT || target == DELETED;
            case ACTIVE -> target == SUSPENDED || target == ARCHIVED;
            case SUSPENDED -> target == ACTIVE || target == ARCHIVED;
            case ARCHIVED -> target == DELETED;
            case DELETED -> false;
        };
    }

    /**
     * Parse a status from its code.
     *
     * @param code the status code
     * @return the matching status
     * @throws IllegalArgumentException if no status matches
     */
    public static Status fromCode(String code) {
        for (Status status : values()) {
            if (status.code.equals(code)) {
                return status;
            }
        }
        throw new IllegalArgumentException("Unknown status code: " + code);
    }
}
