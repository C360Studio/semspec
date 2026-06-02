package org.sensorhub.impl.sensor.mavsdk;

import java.io.IOException;
import java.util.List;
import java.util.Objects;
import java.util.OptionalInt;

/**
 * Owns the lifecycle of the MAVSDK sidecar process used by the MAVLink driver.
 */
public final class MavsdkServerLifecycle {
    private static final int DEFAULT_MAVSDK_GRPC_PORT = 50_051;

    private final ProcessLauncher processLauncher;
    private final String executablePath;
    private final String mavlinkUdpEndpoint;
    private final int grpcPort;
    private final int maxRestartAttempts;
    private ManagedProcess process;
    private DriverStatus status = DriverStatus.STOPPED;

    public MavsdkServerLifecycle(ProcessLauncher processLauncher, String sitlHost, int sitlPort) {
        this(
                processLauncher,
                "mavsdk_server",
                "udp://" + validateHost(sitlHost) + ":" + validatePort(sitlPort),
                DEFAULT_MAVSDK_GRPC_PORT,
                0);
    }

    public MavsdkServerLifecycle(
            ProcessLauncher processLauncher,
            String executablePath,
            String mavlinkUdpEndpoint,
            int grpcPort,
            int maxRestartAttempts) {
        this.processLauncher = Objects.requireNonNull(processLauncher, "processLauncher");
        this.executablePath = requireText(executablePath, "executablePath");
        this.mavlinkUdpEndpoint = validateEndpoint(mavlinkUdpEndpoint);
        this.grpcPort = validatePort(grpcPort);
        if (maxRestartAttempts < 0) {
            throw new IllegalArgumentException("maxRestartAttempts must not be negative");
        }
        this.maxRestartAttempts = maxRestartAttempts;
    }

    public void start() throws IOException {
        if (isRunning()) {
            status = DriverStatus.CONNECTED;
            return;
        }
        startWithRestartBudget(maxRestartAttempts);
    }

    public boolean restartIfExited() throws IOException {
        if (process == null || process.isAlive()) {
            return false;
        }
        OptionalInt exitCode = process.exitCode();
        if (exitCode.isPresent() && exitCode.getAsInt() == 0) {
            status = DriverStatus.STOPPED;
            return false;
        }
        startWithRestartBudget(maxRestartAttempts);
        return true;
    }

    public void shutdown() {
        if (process == null) {
            status = DriverStatus.STOPPED;
            return;
        }
        status = DriverStatus.SHUTTING_DOWN;
        process.close();
        process = null;
        status = DriverStatus.STOPPED;
    }

    public boolean isRunning() {
        return process != null && process.isAlive();
    }

    public DriverStatus status() {
        return status;
    }

    public List<String> command() {
        return List.of(
                executablePath,
                "-p",
                Integer.toString(grpcPort),
                mavlinkUdpEndpoint);
    }

    private void startWithRestartBudget(int restartBudget) throws IOException {
        status = DriverStatus.STARTING;
        IOException lastFailure = null;
        for (int attempt = 0; attempt <= restartBudget; attempt++) {
            process = processLauncher.launch(command());
            if (process.isAlive()) {
                status = DriverStatus.CONNECTED;
                return;
            }
            lastFailure = exitedEarlyFailure(process);
        }
        status = DriverStatus.FAILED;
        throw lastFailure;
    }

    private static IOException exitedEarlyFailure(ManagedProcess process) {
        OptionalInt exitCode = process.exitCode();
        if (exitCode.isPresent()) {
            return new IOException("mavsdk_server exited with code " + exitCode.getAsInt());
        }
        return new IOException("mavsdk_server exited before reporting ready");
    }

    private static String validateEndpoint(String endpoint) {
        String value = requireText(endpoint, "mavlinkUdpEndpoint");
        if (!value.startsWith("udp://")) {
            throw new IllegalArgumentException("mavlinkUdpEndpoint must start with udp://");
        }
        return value;
    }

    private static String validateHost(String host) {
        return requireText(host, "sitlHost");
    }

    private static int validatePort(int port) {
        if (port < 1 || port > 65_535) {
            throw new IllegalArgumentException("port must be between 1 and 65535");
        }
        return port;
    }

    private static String requireText(String value, String name) {
        if (value == null || value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }

    public enum DriverStatus {
        STOPPED,
        STARTING,
        CONNECTED,
        SHUTTING_DOWN,
        FAILED
    }

    @FunctionalInterface
    public interface ProcessLauncher {
        ManagedProcess launch(List<String> command) throws IOException;
    }

    public interface ManagedProcess extends AutoCloseable {
        boolean isAlive();

        default OptionalInt exitCode() {
            return OptionalInt.empty();
        }

        @Override
        void close();
    }
}
