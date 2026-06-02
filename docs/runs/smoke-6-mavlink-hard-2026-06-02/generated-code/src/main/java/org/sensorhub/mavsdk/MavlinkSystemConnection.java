package org.sensorhub.mavsdk;

import java.time.Instant;
import java.util.Arrays;
import java.util.Objects;

record MavlinkSystemConnection(boolean connected, int systemId, int componentId, byte[] rawHeartbeat, Instant observedAt) {
    MavlinkSystemConnection {
        Objects.requireNonNull(rawHeartbeat, "rawHeartbeat must not be null");
        Objects.requireNonNull(observedAt, "observedAt must not be null");
        if (!connected) {
            throw new IllegalArgumentException("connected MAVLink system connection must be true");
        }
        if (systemId < 1 || systemId > 255) {
            throw new IllegalArgumentException("systemId must be in MAVLink uint8 range");
        }
        if (componentId < 0 || componentId > 255) {
            throw new IllegalArgumentException("componentId must be in MAVLink uint8 range");
        }
        rawHeartbeat = Arrays.copyOf(rawHeartbeat, rawHeartbeat.length);
    }

    @Override
    public byte[] rawHeartbeat() {
        return Arrays.copyOf(rawHeartbeat, rawHeartbeat.length);
    }
}
