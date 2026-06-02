package org.sensorhub.mavsdk;

import java.time.Instant;
import java.util.Map;
import java.util.Objects;

record OgcDataStreamObservation(String dataStreamId, String propertyName, Instant phenomenonTime, Map<String, Object> result) {
    OgcDataStreamObservation {
        dataStreamId = requireText(dataStreamId, "dataStreamId");
        propertyName = requireText(propertyName, "propertyName");
        phenomenonTime = Objects.requireNonNull(phenomenonTime, "phenomenonTime must not be null");
        result = Map.copyOf(Objects.requireNonNull(result, "result must not be null"));
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
