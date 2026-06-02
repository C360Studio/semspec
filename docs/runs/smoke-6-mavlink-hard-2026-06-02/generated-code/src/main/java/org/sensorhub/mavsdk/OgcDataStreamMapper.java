package org.sensorhub.mavsdk;

import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Objects;

final class OgcDataStreamMapper {
    private OgcDataStreamMapper() {
    }

    static OgcDataStreamObservation fromHeartbeat(String dataStreamId, MavlinkSystemConnection connection) {
        Objects.requireNonNull(connection, "connection must not be null");
        Map<String, Object> result = new LinkedHashMap<>();
        result.put("systemId", connection.systemId());
        result.put("componentId", connection.componentId());
        result.put("mavsdk_core_connected", connection.connected());
        result.put("rawHeartbeatLength", connection.rawHeartbeat().length);
        return new OgcDataStreamObservation(dataStreamId, "HEARTBEAT", connection.observedAt(), result);
    }
}
