package com.example.annotations;

import java.lang.annotation.*;

/**
 * Annotation to mark methods that should have their execution logged.
 *
 * When applied to a method, an aspect or interceptor should log:
 * - Method entry with parameters
 * - Method exit with return value
 * - Method exit with exception (if thrown)
 *
 * @see java.lang.annotation.Documented
 */
@Documented
@Retention(RetentionPolicy.RUNTIME)
@Target({ElementType.METHOD, ElementType.TYPE})
public @interface Logged {

    /**
     * The log level to use.
     *
     * @return log level name
     */
    String level() default "INFO";

    /**
     * Whether to include parameter values in the log.
     *
     * @return true to log parameters
     */
    boolean includeParameters() default true;

    /**
     * Whether to include return value in the log.
     *
     * @return true to log return value
     */
    boolean includeReturnValue() default false;

    /**
     * Whether to include execution time.
     *
     * @return true to log timing
     */
    boolean includeTime() default true;
}
