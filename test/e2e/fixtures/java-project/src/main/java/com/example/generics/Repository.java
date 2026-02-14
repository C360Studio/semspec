package com.example.generics;

import java.util.List;
import java.util.Optional;

/**
 * Generic repository interface for data access operations.
 *
 * Provides a standard interface for CRUD operations that can be
 * implemented for different storage backends.
 *
 * @param <T> the entity type
 * @param <ID> the type of the entity identifier
 */
public interface Repository<T, ID> {

    /**
     * Find an entity by its ID.
     *
     * @param id the entity identifier
     * @return the entity if found
     */
    Optional<T> findById(ID id);

    /**
     * Find all entities.
     *
     * @return list of all entities
     */
    List<T> findAll();

    /**
     * Save an entity (create or update).
     *
     * @param entity the entity to save
     * @return the saved entity
     */
    T save(T entity);

    /**
     * Delete an entity by ID.
     *
     * @param id the entity identifier
     * @return true if deleted, false if not found
     */
    boolean deleteById(ID id);

    /**
     * Check if an entity exists.
     *
     * @param id the entity identifier
     * @return true if exists
     */
    boolean existsById(ID id);

    /**
     * Count total entities.
     *
     * @return number of entities
     */
    long count();

    /**
     * Delete all entities.
     */
    void deleteAll();
}
