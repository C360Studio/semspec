package org.sensorhub.mavsdk;

import java.util.Objects;

record MavsdkJavaClientConnection(
        boolean mavsdkJavaClientConnected,
        DriverStatus driverStatus,
        MavlinkSystemConnection connection) {
    MavsdkJavaClientConnection {
        driverStatus = Objects.requireNonNull(driverStatus, "driverStatus must not be null");
        connection = Objects.requireNonNull(connection, "connection must not be null");
    }
}
