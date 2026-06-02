package org.sensorhub.mavsdk;

import java.io.IOException;
import java.time.Duration;
import java.util.Objects;

final class MavsdkJavaClientConnectionProbe {
    private static final Duration POLL_TIMEOUT = Duration.ofMillis(100);

    private final SitlUdpEndpoint endpoint;
    private final Duration connectionTimeout;
    private final CoreConnectionClient coreConnectionClient;

    MavsdkJavaClientConnectionProbe(SitlUdpEndpoint endpoint, Duration connectionTimeout) {
        this(endpoint, connectionTimeout, new HeartbeatCoreConnectionClient());
    }

    MavsdkJavaClientConnectionProbe(
            SitlUdpEndpoint endpoint,
            Duration connectionTimeout,
            CoreConnectionClient coreConnectionClient) {
        this.endpoint = Objects.requireNonNull(endpoint, "endpoint must not be null");
        this.connectionTimeout = Objects.requireNonNull(connectionTimeout, "connectionTimeout must not be null");
        this.coreConnectionClient = Objects.requireNonNull(coreConnectionClient, "coreConnectionClient must not be null");
        if (connectionTimeout.isZero() || connectionTimeout.isNegative()) {
            throw new IllegalArgumentException("connectionTimeout must be positive");
        }
    }

    MavsdkJavaClientConnection awaitConnected() throws IOException {
        MavlinkSystemConnection connection = coreConnectionClient.awaitConnected(endpoint, connectionTimeout);
        return new MavsdkJavaClientConnection(true, DriverStatus.CONNECTED, connection);
    }

    @FunctionalInterface
    interface CoreConnectionClient {
        MavlinkSystemConnection awaitConnected(SitlUdpEndpoint endpoint, Duration timeout) throws IOException;
    }

    private static final class HeartbeatCoreConnectionClient implements CoreConnectionClient {
        @Override
        public MavlinkSystemConnection awaitConnected(SitlUdpEndpoint endpoint, Duration timeout) throws IOException {
            MavlinkSystemConnector connector = new MavlinkSystemConnector(
                endpoint.host(),
                endpoint.port(),
                POLL_TIMEOUT,
                maxAttempts(timeout, POLL_TIMEOUT));
            return connector.awaitConnectedVehicle();
        }
    }

    private static int maxAttempts(Duration timeout, Duration interval) {
        long attempts = Math.max(1L, timeout.toMillis() / interval.toMillis());
        return attempts > Integer.MAX_VALUE ? Integer.MAX_VALUE : (int) attempts;
    }
}
