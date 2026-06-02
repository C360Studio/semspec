package org.sensorhub.mavsdk;

import java.io.IOException;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Objects;
import java.util.OptionalInt;

final class MavsdkServerManager implements AutoCloseable {
    private final MavsdkServerConfig config;
    private final ProcessLauncher processLauncher;
    private ManagedMavsdkServer server;
    private DriverStatus status = DriverStatus.STOPPED;

    MavsdkServerManager(MavsdkServerConfig config) {
        this(config, new ProcessBuilderLauncher());
    }

    MavsdkServerManager(MavsdkServerConfig config, ProcessLauncher processLauncher) {
        this.config = Objects.requireNonNull(config, "config must not be null");
        this.processLauncher = Objects.requireNonNull(processLauncher, "processLauncher must not be null");
    }

    ManagedMavsdkServer startServerProcess() throws IOException {
        if (server != null && server.isAlive()) {
            status = DriverStatus.CONNECTED;
            return server;
        }
        return launchWithRestartBudget(config.maxRestartAttempts());
    }

    boolean restartIfExited() throws IOException {
        if (server == null || server.isAlive()) {
            return false;
        }
        OptionalInt exitCode = server.exitCode();
        if (exitCode.isPresent() && exitCode.getAsInt() == 0) {
            status = DriverStatus.STOPPED;
            return false;
        }
        launchWithRestartBudget(config.maxRestartAttempts());
        return true;
    }

    DriverStatus status() {
        return status;
    }

    List<String> command() {
        List<String> command = new ArrayList<>(1 + config.arguments().size());
        command.add(config.executable().toString());
        command.addAll(config.arguments());
        return List.copyOf(command);
    }

    @Override
    public void close() {
        if (server != null) {
            server.close();
            server = null;
        }
        status = DriverStatus.STOPPED;
    }

    private ManagedMavsdkServer launchWithRestartBudget(int restartBudget) throws IOException {
        status = DriverStatus.STARTING;
        IOException lastFailure = null;
        for (int attempt = 0; attempt <= restartBudget; attempt++) {
            server = processLauncher.launch(command());
            lastFailure = waitForConnectedOrExited(server);
            if (lastFailure == null) {
                status = DriverStatus.CONNECTED;
                return server;
            }
            server.close();
        }
        status = DriverStatus.FAILED;
        throw lastFailure;
    }

    private IOException waitForConnectedOrExited(ManagedMavsdkServer launchedServer) throws IOException {
        int maxAttempts = maxAttempts(config.startupTimeout(), config.readinessPollInterval());
        for (int attempt = 0; attempt < maxAttempts; attempt++) {
            if (!launchedServer.isAlive()) {
                return exitedEarlyFailure(launchedServer);
            }
            sleep(config.readinessPollInterval());
        }
        return null;
    }

    private static IOException exitedEarlyFailure(ManagedMavsdkServer launchedServer) {
        OptionalInt exitCode = launchedServer.exitCode();
        if (exitCode.isPresent()) {
            return new IOException("mavsdk_server exited with code " + exitCode.getAsInt());
        }
        return new IOException("mavsdk_server exited before startup completed");
    }

    private static int maxAttempts(Duration timeout, Duration interval) {
        long attempts = Math.max(1L, timeout.toMillis() / interval.toMillis());
        return attempts > Integer.MAX_VALUE ? Integer.MAX_VALUE : (int) attempts;
    }

    private static void sleep(Duration duration) throws IOException {
        try {
            Thread.sleep(duration.toMillis());
        } catch (InterruptedException error) {
            Thread.currentThread().interrupt();
            throw new IOException("interrupted while waiting for mavsdk_server startup", error);
        }
    }

    @FunctionalInterface
    interface ProcessLauncher {
        ManagedMavsdkServer launch(List<String> command) throws IOException;
    }

    private static final class ProcessBuilderLauncher implements ProcessLauncher {
        @Override
        public ManagedMavsdkServer launch(List<String> command) throws IOException {
            if (command.isEmpty()) {
                throw new IllegalArgumentException("mavsdk_server command must not be empty");
            }
            Process process = new ProcessBuilder(command)
                .redirectErrorStream(true)
                .start();
            return new ManagedMavsdkServer(java.nio.file.Path.of(command.get(0)), process);
        }
    }
}
