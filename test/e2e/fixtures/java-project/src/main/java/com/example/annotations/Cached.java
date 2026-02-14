package com.example.annotations;

import java.lang.annotation.*;
import java.util.concurrent.TimeUnit;

/**
 * Annotation to mark methods whose results should be cached.
 *
 * The cache key is computed from the method parameters.
 * Results are stored and returned for subsequent calls with
 * the same parameters.
 */
@Documented
@Retention(RetentionPolicy.RUNTIME)
@Target(ElementType.METHOD)
public @interface Cached {

    /**
     * The cache name/region to use.
     *
     * @return cache name
     */
    String value() default "default";

    /**
     * Time to live for cached entries.
     *
     * @return TTL value
     */
    long ttl() default 3600;

    /**
     * Time unit for TTL.
     *
     * @return time unit
     */
    TimeUnit timeUnit() default TimeUnit.SECONDS;

    /**
     * Maximum number of entries in the cache.
     *
     * @return max entries (0 for unlimited)
     */
    int maxSize() default 1000;

    /**
     * Whether to refresh entries before expiration.
     *
     * @return true to enable refresh
     */
    boolean refreshAhead() default false;

    /**
     * SpEL expression for cache key.
     * If empty, uses method parameters.
     *
     * @return key expression
     */
    String key() default "";

    /**
     * Condition for caching (SpEL expression).
     *
     * @return condition expression
     */
    String condition() default "";
}
