package org.sensorhub.mavsdk;

import java.util.Objects;

record SitlUdpEndpoint(String host, int port) {
    SitlUdpEndpoint {
        host = requireHost(host);
        if (port < 1 || port > 65_535) {
            throw new IllegalArgumentException("SITL UDP endpoint port must be between 1 and 65535");
        }
    }

    static SitlUdpEndpoint fromEnv(String envName) {
        String name = requireHost(envName);
        String value = System.getenv(name);
        if (value == null || value.isBlank()) {
            throw new IllegalStateException(name + " must be provided by the mavlink.px4-sitl.mavsdk-smoke harness");
        }
        return parse(value);
    }

    static SitlUdpEndpoint parse(String endpoint) {
        String value = Objects.requireNonNull(endpoint, "endpoint must not be null").trim();
        if (value.isEmpty()) {
            throw new IllegalArgumentException("SITL UDP endpoint must not be blank");
        }
        if (value.startsWith("udp://")) {
            value = value.substring("udp://".length());
        }
        int separator = value.lastIndexOf(':');
        if (separator < 1 || separator == value.length() - 1) {
            throw new IllegalArgumentException("SITL UDP endpoint must be host:port or udp://host:port");
        }
        return new SitlUdpEndpoint(value.substring(0, separator), parsePort(value.substring(separator + 1)));
    }

    private static int parsePort(String rawPort) {
        try {
            return Integer.parseInt(rawPort);
        } catch (NumberFormatException error) {
            throw new IllegalArgumentException("SITL UDP endpoint port must be numeric", error);
        }
    }

    private static String requireHost(String value) {
        if (value == null || value.isBlank()) {
            throw new IllegalArgumentException("SITL UDP endpoint host must not be blank");
        }
        return value;
    }
}
