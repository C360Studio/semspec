package com.example.models;

import java.time.Instant;
import java.util.Objects;
import java.util.UUID;

/**
 * Abstract base class for all domain entities.
 *
 * Provides common functionality like ID generation, equality checking,
 * and timestamp management.
 *
 * @param <ID> the type of the entity identifier
 */
public abstract class BaseEntity<ID> {

    private final ID id;
    private Instant createdAt;
    private Instant updatedAt;
    private int version;

    /**
     * Create a new entity with the given ID.
     *
     * @param id the entity identifier
     */
    protected BaseEntity(ID id) {
        this.id = Objects.requireNonNull(id, "id must not be null");
        this.createdAt = Instant.now();
        this.version = 1;
    }

    /**
     * Get the entity identifier.
     *
     * @return the ID
     */
    public ID getId() {
        return id;
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
     * Get the last update timestamp.
     *
     * @return update time or null if never updated
     */
    public Instant getUpdatedAt() {
        return updatedAt;
    }

    /**
     * Get the entity version for optimistic locking.
     *
     * @return current version
     */
    public int getVersion() {
        return version;
    }

    /**
     * Update the entity's timestamp and increment version.
     * Should be called before persisting changes.
     */
    protected void touch() {
        this.updatedAt = Instant.now();
        this.version++;
    }

    /**
     * Validate the entity state.
     * Subclasses should override to add specific validation logic.
     *
     * @return true if the entity is valid
     */
    public abstract boolean validate();

    /**
     * Get the age of this entity in seconds.
     *
     * @return seconds since creation
     */
    public long getAgeSeconds() {
        return Instant.now().getEpochSecond() - createdAt.getEpochSecond();
    }

    @Override
    public boolean equals(Object o) {
        if (this == o) return true;
        if (o == null || getClass() != o.getClass()) return false;
        BaseEntity<?> that = (BaseEntity<?>) o;
        return Objects.equals(id, that.id);
    }

    @Override
    public int hashCode() {
        return Objects.hash(id);
    }

    /**
     * Generate a new UUID-based string identifier.
     *
     * @return new unique ID
     */
    protected static String generateId() {
        return UUID.randomUUID().toString();
    }
}
