package com.example.annotations;

import java.lang.annotation.*;

/**
 * Annotation to mark methods that should be retried on failure.
 *
 * Configures automatic retry behavior for transient failures.
 */
@Documented
@Retention(RetentionPolicy.RUNTIME)
@Target(ElementType.METHOD)
public @interface Retry {

    /**
     * Maximum number of retry attempts.
     *
     * @return max attempts (must be >= 1)
     */
    int maxAttempts() default 3;

    /**
     * Initial delay between retries in milliseconds.
     *
     * @return initial delay
     */
    long delay() default 1000;

    /**
     * Multiplier for exponential backoff.
     *
     * @return backoff multiplier
     */
    double backoff() default 2.0;

    /**
     * Maximum delay between retries in milliseconds.
     *
     * @return max delay
     */
    long maxDelay() default 30000;

    /**
     * Exception types that should trigger a retry.
     *
     * @return retryable exception types
     */
    Class<? extends Throwable>[] retryOn() default {Exception.class};

    /**
     * Exception types that should NOT trigger a retry.
     *
     * @return non-retryable exception types
     */
    Class<? extends Throwable>[] noRetryOn() default {};

    /**
     * Whether to add jitter to the delay.
     *
     * @return true to add random jitter
     */
    boolean jitter() default true;
}
