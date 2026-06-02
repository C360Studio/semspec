package org.sensorhub.mavsdk;

import io.mavsdk.core.Core;
import io.reactivex.disposables.Disposable;
import java.time.Duration;
import java.util.Objects;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;

final class MavsdkJavaSystemClient implements AutoCloseable {
    private final String host;
    private final int port;
    private final io.mavsdk.System system;

    MavsdkJavaSystemClient(String host, int port) {
        this(host, port, new io.mavsdk.System(requireHost(host), validatePort(port)));
    }

    MavsdkJavaSystemClient(String host, int port, io.mavsdk.System system) {
        this.host = requireHost(host);
        this.port = validatePort(port);
        this.system = Objects.requireNonNull(system, "system must not be null");
    }

    boolean awaitCoreConnected(Duration timeout) throws InterruptedException {
        Objects.requireNonNull(timeout, "timeout must not be null");
        if (timeout.isZero() || timeout.isNegative()) {
            throw new IllegalArgumentException("timeout must be positive");
        }
        CountDownLatch connected = new CountDownLatch(1);
        AtomicBoolean sawConnected = new AtomicBoolean(false);
        Core core = system.getCore();
        core.initialize();
        Disposable subscription = core.getConnectionState().subscribe(state -> {
            if (Boolean.TRUE.equals(state.getIsConnected())) {
                sawConnected.set(true);
                connected.countDown();
            }
        });
        try {
            if (!sawConnected.get()) {
                connected.await(timeout.toMillis(), TimeUnit.MILLISECONDS);
            }
            return sawConnected.get();
        } finally {
            subscription.dispose();
            core.dispose();
        }
    }

    String host() {
        return host;
    }

    int port() {
        return port;
    }

    @Override
    public void close() {
        system.dispose();
    }

    private static String requireHost(String value) {
        if (value == null || value.isBlank()) {
            throw new IllegalArgumentException("MAVSDK Java client host must not be blank");
        }
        return value;
    }

    private static int validatePort(int value) {
        if (value < 1 || value > 65_535) {
            throw new IllegalArgumentException("MAVSDK Java client port must be between 1 and 65535");
        }
        return value;
    }
}
