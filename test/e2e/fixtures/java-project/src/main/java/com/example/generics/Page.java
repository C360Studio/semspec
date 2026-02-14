package com.example.generics;

import java.util.List;
import java.util.Objects;
import java.util.function.Function;
import java.util.stream.Collectors;

/**
 * A page of results for paginated queries.
 *
 * @param <T> the type of items in the page
 */
public record Page<T>(
        List<T> items,
        int pageNumber,
        int pageSize,
        long totalItems,
        int totalPages
) {
    /**
     * Create a page of results.
     *
     * @param items the items in this page
     * @param pageNumber current page number (1-indexed)
     * @param pageSize number of items per page
     * @param totalItems total number of items across all pages
     * @param totalPages total number of pages
     */
    public Page {
        items = List.copyOf(items);
        if (pageNumber < 1) {
            throw new IllegalArgumentException("pageNumber must be >= 1");
        }
        if (pageSize < 1) {
            throw new IllegalArgumentException("pageSize must be >= 1");
        }
    }

    /**
     * Check if there is a next page.
     *
     * @return true if not on the last page
     */
    public boolean hasNext() {
        return pageNumber < totalPages;
    }

    /**
     * Check if there is a previous page.
     *
     * @return true if not on the first page
     */
    public boolean hasPrevious() {
        return pageNumber > 1;
    }

    /**
     * Check if this is the first page.
     *
     * @return true if on page 1
     */
    public boolean isFirst() {
        return pageNumber == 1;
    }

    /**
     * Check if this is the last page.
     *
     * @return true if on the last page
     */
    public boolean isLast() {
        return pageNumber == totalPages;
    }

    /**
     * Get the number of items in this page.
     *
     * @return number of items
     */
    public int size() {
        return items.size();
    }

    /**
     * Check if this page is empty.
     *
     * @return true if no items
     */
    public boolean isEmpty() {
        return items.isEmpty();
    }

    /**
     * Transform the items in this page.
     *
     * @param mapper transformation function
     * @param <R> the result type
     * @return a new page with transformed items
     */
    public <R> Page<R> map(Function<T, R> mapper) {
        Objects.requireNonNull(mapper, "mapper must not be null");
        List<R> mapped = items.stream()
                .map(mapper)
                .collect(Collectors.toList());
        return new Page<>(mapped, pageNumber, pageSize, totalItems, totalPages);
    }

    /**
     * Create an empty page.
     *
     * @param pageSize the page size
     * @param <T> item type
     * @return empty page
     */
    public static <T> Page<T> empty(int pageSize) {
        return new Page<>(List.of(), 1, pageSize, 0, 0);
    }

    /**
     * Create a page from a list with pagination info.
     *
     * @param items all items (will be sliced)
     * @param pageNumber the page to extract
     * @param pageSize items per page
     * @param <T> item type
     * @return the requested page
     */
    public static <T> Page<T> of(List<T> items, int pageNumber, int pageSize) {
        int total = items.size();
        int totalPages = (int) Math.ceil((double) total / pageSize);

        int start = (pageNumber - 1) * pageSize;
        int end = Math.min(start + pageSize, total);

        List<T> pageItems = (start < total)
                ? items.subList(start, end)
                : List.of();

        return new Page<>(pageItems, pageNumber, pageSize, total, totalPages);
    }
}
