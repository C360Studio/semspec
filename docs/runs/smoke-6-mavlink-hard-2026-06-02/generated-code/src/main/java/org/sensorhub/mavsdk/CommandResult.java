package org.sensorhub.mavsdk;

import java.time.Instant;
import java.util.Objects;

record CommandResult(String commandId, CommandStatus status, String message, Instant completedAt) {
    CommandResult {
        commandId = requireText(commandId, "commandId");
        status = Objects.requireNonNull(status, "status must not be null");
        message = requireText(message, "message");
        completedAt = Objects.requireNonNull(completedAt, "completedAt must not be null");
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
