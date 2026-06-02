package org.sensorhub.mavsdk;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.time.Duration;
import java.util.concurrent.atomic.AtomicBoolean;
import org.junit.jupiter.api.Test;

class MavsdkJavaClientConnectionProbeTest {
    @Test
    void unitProbeRequiresMavsdkCoreConnectionBeforeReportingDriverConnectedWithinThirtySeconds() throws Exception {
        // @unit scenario: MAVSDK Java client gate must pass SITL_UDP_ENDPOINT and the 30 second bound.
        SitlUdpEndpoint endpoint = SitlUdpEndpoint.parse("udp://sitl.example.test:14540");
        RecordingCoreConnectionClient coreClient = new RecordingCoreConnectionClient(
            MavlinkHeartbeat.parse(MavlinkHeartbeat.v1(42, 1), 17).orElseThrow());
        MavsdkJavaClientConnectionProbe probe = new MavsdkJavaClientConnectionProbe(
            endpoint,
            Duration.ofSeconds(30),
            coreClient);

        MavsdkJavaClientConnection connection = probe.awaitConnected();

        assertTrue(connection.mavsdkJavaClientConnected(), "MAVSDK Java client should be reported connected only after core gate");
        assertEquals(DriverStatus.CONNECTED, connection.driverStatus(), "driver status should become CONNECTED after core gate");
        assertTrue(connection.connection().connected(), "mavsdk_core_connected heartbeat state must be preserved");
        assertEquals(endpoint, coreClient.endpointSeen);
        assertEquals(Duration.ofSeconds(30), coreClient.timeoutSeen);
    }

    @Test
    void unitProbeDoesNotReportConnectedWhenMavsdkCoreClientFails() {
        AtomicBoolean called = new AtomicBoolean(false);
        MavsdkJavaClientConnectionProbe probe = new MavsdkJavaClientConnectionProbe(
            new SitlUdpEndpoint("sitl.example.test", 14540),
            Duration.ofSeconds(30),
            (endpoint, timeout) -> {
                called.set(true);
                throw new IOException("core not connected");
            });

        IOException error = assertThrows(IOException.class, probe::awaitConnected);

        assertTrue(called.get(), "probe must attempt the MAVSDK Java Core connection gate");
        assertEquals("core not connected", error.getMessage());
    }

    @Test
    void unitProbeValidatesRequiredInputs() {
        MavsdkJavaClientConnectionProbe.CoreConnectionClient coreClient =
            (endpoint, timeout) -> MavlinkHeartbeat.parse(MavlinkHeartbeat.v1(1, 1), 17).orElseThrow();

        assertThrows(NullPointerException.class, () -> new MavsdkJavaClientConnectionProbe(
            null,
            Duration.ofSeconds(30),
            coreClient));
        assertThrows(IllegalArgumentException.class, () -> new MavsdkJavaClientConnectionProbe(
            new SitlUdpEndpoint("sitl.example.test", 14540),
            Duration.ZERO,
            coreClient));
        assertThrows(NullPointerException.class, () -> new MavsdkJavaClientConnectionProbe(
            new SitlUdpEndpoint("sitl.example.test", 14540),
            Duration.ofSeconds(30),
            null));
    }

    private static final class RecordingCoreConnectionClient implements MavsdkJavaClientConnectionProbe.CoreConnectionClient {
        private final MavlinkSystemConnection connection;
        private SitlUdpEndpoint endpointSeen;
        private Duration timeoutSeen;

        RecordingCoreConnectionClient(MavlinkSystemConnection connection) {
            this.connection = connection;
        }

        @Override
        public MavlinkSystemConnection awaitConnected(SitlUdpEndpoint endpoint, Duration timeout) {
            endpointSeen = endpoint;
            timeoutSeen = timeout;
            return connection;
        }
    }
}
