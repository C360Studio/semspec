package com.example.models;

import java.time.Instant;
import java.util.Objects;

/**
 * Audit information for tracking entity changes.
 *
 * This class is designed to be embedded in other entities
 * to track who made changes and when.
 */
public class AuditInfo {

    private final String createdBy;
    private final Instant createdAt;
    private String updatedBy;
    private Instant updatedAt;

    /**
     * Create audit info for a newly created entity.
     *
     * @param createdBy ID of the user who created the entity
     */
    public AuditInfo(String createdBy) {
        this.createdBy = Objects.requireNonNull(createdBy, "createdBy must not be null");
        this.createdAt = Instant.now();
    }

    /**
     * Record an update to the entity.
     *
     * @param updatedBy ID of the user who made the update
     */
    public void recordUpdate(String updatedBy) {
        this.updatedBy = Objects.requireNonNull(updatedBy, "updatedBy must not be null");
        this.updatedAt = Instant.now();
    }

    /**
     * Get the ID of the user who created the entity.
     *
     * @return creator user ID
     */
    public String getCreatedBy() {
        return createdBy;
    }

    /**
     * Get the creation timestamp.
     *
     * @return creation time
     */
    public Instant getCreatedAt() {
        return createdAt;
    }

    /**
     * Get the ID of the user who last updated the entity.
     *
     * @return updater user ID or null if never updated
     */
    public String getUpdatedBy() {
        return updatedBy;
    }

    /**
     * Get the last update timestamp.
     *
     * @return update time or null if never updated
     */
    public Instant getUpdatedAt() {
        return updatedAt;
    }

    /**
     * Check if the entity has been updated since creation.
     *
     * @return true if updated at least once
     */
    public boolean hasBeenUpdated() {
        return updatedAt != null;
    }

    @Override
    public boolean equals(Object o) {
        if (this == o) return true;
        if (o == null || getClass() != o.getClass()) return false;
        AuditInfo auditInfo = (AuditInfo) o;
        return Objects.equals(createdBy, auditInfo.createdBy) &&
               Objects.equals(createdAt, auditInfo.createdAt);
    }

    @Override
    public int hashCode() {
        return Objects.hash(createdBy, createdAt);
    }

    @Override
    public String toString() {
        return "AuditInfo{" +
                "createdBy='" + createdBy + '\'' +
                ", createdAt=" + createdAt +
                ", updatedBy='" + updatedBy + '\'' +
                ", updatedAt=" + updatedAt +
                '}';
    }

    /**
     * Inner class for tracking individual changes.
     */
    public static class ChangeRecord {
        private final String field;
        private final Object oldValue;
        private final Object newValue;
        private final Instant changedAt;
        private final String changedBy;

        /**
         * Create a change record.
         *
         * @param field the field that was changed
         * @param oldValue the previous value
         * @param newValue the new value
         * @param changedBy who made the change
         */
        public ChangeRecord(String field, Object oldValue, Object newValue, String changedBy) {
            this.field = field;
            this.oldValue = oldValue;
            this.newValue = newValue;
            this.changedAt = Instant.now();
            this.changedBy = changedBy;
        }

        public String getField() {
            return field;
        }

        public Object getOldValue() {
            return oldValue;
        }

        public Object getNewValue() {
            return newValue;
        }

        public Instant getChangedAt() {
            return changedAt;
        }

        public String getChangedBy() {
            return changedBy;
        }
    }
}
