package com.example.generics;

import java.util.*;
import java.util.concurrent.ConcurrentHashMap;
import java.util.function.Function;
import java.util.function.Predicate;
import java.util.stream.Collectors;

/**
 * In-memory implementation of the Repository interface.
 *
 * Stores entities in a concurrent hash map for thread-safe access.
 * Useful for testing and development environments.
 *
 * @param <T> the entity type
 * @param <ID> the type of the entity identifier
 */
public class InMemoryRepository<T, ID> implements Repository<T, ID> {

    private final Map<ID, T> storage = new ConcurrentHashMap<>();
    private final Function<T, ID> idExtractor;

    /**
     * Create a new in-memory repository.
     *
     * @param idExtractor function to extract ID from an entity
     */
    public InMemoryRepository(Function<T, ID> idExtractor) {
        this.idExtractor = Objects.requireNonNull(idExtractor, "idExtractor must not be null");
    }

    @Override
    public Optional<T> findById(ID id) {
        return Optional.ofNullable(storage.get(id));
    }

    @Override
    public List<T> findAll() {
        return new ArrayList<>(storage.values());
    }

    @Override
    public T save(T entity) {
        ID id = idExtractor.apply(entity);
        storage.put(id, entity);
        return entity;
    }

    @Override
    public boolean deleteById(ID id) {
        return storage.remove(id) != null;
    }

    @Override
    public boolean existsById(ID id) {
        return storage.containsKey(id);
    }

    @Override
    public long count() {
        return storage.size();
    }

    @Override
    public void deleteAll() {
        storage.clear();
    }

    /**
     * Find entities matching a predicate.
     *
     * @param predicate filter condition
     * @return matching entities
     */
    public List<T> findBy(Predicate<T> predicate) {
        return storage.values().stream()
                .filter(predicate)
                .collect(Collectors.toList());
    }

    /**
     * Find the first entity matching a predicate.
     *
     * @param predicate filter condition
     * @return first matching entity if found
     */
    public Optional<T> findFirst(Predicate<T> predicate) {
        return storage.values().stream()
                .filter(predicate)
                .findFirst();
    }

    /**
     * Save multiple entities.
     *
     * @param entities entities to save
     * @return saved entities
     */
    public List<T> saveAll(Iterable<T> entities) {
        List<T> result = new ArrayList<>();
        for (T entity : entities) {
            result.add(save(entity));
        }
        return result;
    }

    /**
     * Delete entities matching a predicate.
     *
     * @param predicate filter condition
     * @return number of deleted entities
     */
    public int deleteBy(Predicate<T> predicate) {
        List<ID> toDelete = storage.entrySet().stream()
                .filter(e -> predicate.test(e.getValue()))
                .map(Map.Entry::getKey)
                .collect(Collectors.toList());

        toDelete.forEach(storage::remove);
        return toDelete.size();
    }
}
