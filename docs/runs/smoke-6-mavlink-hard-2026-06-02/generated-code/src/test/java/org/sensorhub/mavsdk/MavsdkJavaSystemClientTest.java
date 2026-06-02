package org.sensorhub.mavsdk;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import io.mavsdk.core.Core;
import io.reactivex.Flowable;
import java.time.Duration;
import java.util.concurrent.atomic.AtomicBoolean;
import org.junit.jupiter.api.Test;

class MavsdkJavaSystemClientTest {
    @Test
    void unitMavsdkJavaClientReportsCoreConnectionStateFromRealMavsdkApiSurface() throws Exception {
        // @unit scenario: MAVSDK Java client connection gate consumes Core.getConnectionState().
        FakeSystem system = new FakeSystem(Flowable.just(new Core.ConnectionState(true)));
        try (MavsdkJavaSystemClient client = new MavsdkJavaSystemClient("127.0.0.1", 50051, system)) {
            boolean connected = client.awaitCoreConnected(Duration.ofMillis(100));

            assertTrue(connected, "mavsdk_core_connected should be true when MAVSDK Core reports connected");
            assertEquals("127.0.0.1", client.host());
            assertEquals(50051, client.port());
            assertTrue(system.core.initialized.get(), "MAVSDK Core plugin must be initialized before subscription");
            assertTrue(system.core.disposed.get(), "MAVSDK Core plugin must be disposed after subscription");
        }
        assertTrue(system.disposed.get(), "MAVSDK Java System must be disposed when the client closes");
    }

    @Test
    void unitMavsdkJavaClientReturnsFalseWhenCoreDoesNotConnectBeforeTimeout() throws Exception {
        FakeSystem system = new FakeSystem(Flowable.never());
        try (MavsdkJavaSystemClient client = new MavsdkJavaSystemClient("127.0.0.1", 50051, system)) {
            boolean connected = client.awaitCoreConnected(Duration.ofMillis(10));

            assertFalse(connected, "MAVSDK Java client should not report connected without Core state");
        }
    }

    @Test
    void unitMavsdkJavaClientValidatesHostPortAndTimeoutInputs() {
        FakeSystem system = new FakeSystem(Flowable.just(new Core.ConnectionState(true)));

        assertThrows(IllegalArgumentException.class, () -> new MavsdkJavaSystemClient(" ", 50051, system));
        assertThrows(IllegalArgumentException.class, () -> new MavsdkJavaSystemClient("127.0.0.1", 0, system));
        try (MavsdkJavaSystemClient client = new MavsdkJavaSystemClient("127.0.0.1", 50051, system)) {
            assertThrows(IllegalArgumentException.class, () -> client.awaitCoreConnected(Duration.ZERO));
        }
    }

    private static final class FakeSystem extends io.mavsdk.System {
        private final FakeCore core;
        private final AtomicBoolean disposed = new AtomicBoolean(false);

        FakeSystem(Flowable<Core.ConnectionState> connectionStates) {
            this.core = new FakeCore(connectionStates);
        }

        @Override
        public Core getCore() {
            return core;
        }

        @Override
        public void dispose() {
            disposed.set(true);
        }
    }

    private static final class FakeCore extends Core {
        private final Flowable<Core.ConnectionState> connectionStates;
        private final AtomicBoolean initialized = new AtomicBoolean(false);
        private final AtomicBoolean disposed = new AtomicBoolean(false);

        FakeCore(Flowable<Core.ConnectionState> connectionStates) {
            this.connectionStates = connectionStates;
        }

        @Override
        public void initialize() {
            initialized.set(true);
        }

        @Override
        public Flowable<Core.ConnectionState> getConnectionState() {
            return connectionStates;
        }

        @Override
        public void dispose() {
            disposed.set(true);
        }
    }
}
