package org.sensorhub.mavsdk;

import java.io.IOException;
import java.time.Instant;
import java.util.Objects;

final class MavsdkActionControl {
    private final MavlinkSystemConnection connection;
    private final MavsdkActionExecutor actionExecutor;

    MavsdkActionControl(MavlinkSystemConnection connection, MavsdkActionExecutor actionExecutor) {
        this.connection = requireConnectedVehicle(connection);
        this.actionExecutor = Objects.requireNonNull(actionExecutor, "actionExecutor must not be null");
    }

    CommandResult executeAction(CommandRequest request) {
        Objects.requireNonNull(request, "request must not be null");
        if (!"HOLD".equals(request.actionName())) {
            return new CommandResult(request.commandId(), CommandStatus.FAILED, "Unsupported MAVSDK action", Instant.now());
        }
        try {
            String message = actionExecutor.executeHold(connection, request);
            return new CommandResult(request.commandId(), CommandStatus.SUCCEEDED, message, Instant.now());
        } catch (IOException error) {
            return new CommandResult(request.commandId(), CommandStatus.FAILED, "MAVSDK action failed", Instant.now());
        } catch (InterruptedException error) {
            Thread.currentThread().interrupt();
            return new CommandResult(request.commandId(), CommandStatus.FAILED, "MAVSDK action interrupted", Instant.now());
        }
    }

    private static MavlinkSystemConnection requireConnectedVehicle(MavlinkSystemConnection connection) {
        if (connection == null || !connection.connected()) {
            throw new IllegalArgumentException("connected vehicle is required before issuing control commands");
        }
        return connection;
    }
}
