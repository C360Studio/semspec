package com.example.annotations;

import java.lang.annotation.*;

/**
 * Annotation to mark methods that require parameter validation.
 *
 * When applied, an aspect or interceptor should validate:
 * - Non-null constraints on parameters
 * - Custom validation rules
 *
 * Can be combined with specific parameter annotations.
 */
@Documented
@Retention(RetentionPolicy.RUNTIME)
@Target({ElementType.METHOD, ElementType.TYPE})
public @interface Validated {

    /**
     * Whether to throw on validation failure.
     * If false, returns null or default value.
     *
     * @return true to throw exception
     */
    boolean failFast() default true;

    /**
     * Custom message for validation failures.
     *
     * @return error message template
     */
    String message() default "Validation failed";
}
