package org.sensorhub.mavsdk;

import java.util.Locale;
import java.util.Objects;

record CommandRequest(String commandId, String actionName) {
    CommandRequest {
        commandId = requireText(commandId, "commandId");
        actionName = requireText(actionName, "actionName").toUpperCase(Locale.ROOT);
    }

    static CommandRequest holdPosition(String commandId) {
        return new CommandRequest(commandId, "HOLD");
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
