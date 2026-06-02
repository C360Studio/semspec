package org.sensorhub.mavsdk;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.InetAddress;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.List;
import java.util.Optional;
import java.util.OptionalInt;
import java.util.Queue;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Assumptions;

class MavsdkServerManagerTest {

    @Test
    void unitStartsMavsdkServerWithConfiguredExecutableUsingInProcessFake() throws Exception {
        // @unit scenario: process builder boundary receives the exact mavsdk_server executable path and args.
        Path executable = writeServerScript("fake-ready");
        RecordingServerProcess running = RecordingServerProcess.running();
        RecordingProcessLauncher launcher = new RecordingProcessLauncher(executable, running);
        MavsdkServerConfig config = new MavsdkServerConfig(
            executable,
            List.of("-p", "50051", "udp://sitl.example.test:14540"),
            Duration.ofMillis(1),
            Duration.ofMillis(2));

        try (MavsdkServerManager manager = new MavsdkServerManager(config, launcher)) {
            ManagedMavsdkServer server = manager.startServerProcess();

            assertTrue(server.isAlive(), "fake mavsdk_server should be alive after startup");
            assertEquals(DriverStatus.CONNECTED, manager.status(), "driver status model should report CONNECTED");
            assertEquals(executable, server.executable());
            assertEquals(
                List.of(executable.toString(), "-p", "50051", "udp://sitl.example.test:14540"),
                launcher.launchedCommands().get(0),
                "configured executable path must be the first ProcessBuilder command element");
        }

        assertFalse(running.isAlive(), "close must terminate the fake in-process mavsdk_server");
    }

    @Test
    void unitRetriesStartupAndReportsFailedWhenNonZeroExitExhaustsRestartBudget() throws Exception {
        // @unit scenario: non-zero mavsdk_server exit triggers bounded retry and FAILED status on exhaustion.
        Path executable = writeServerScript("fake-ready");
        RecordingServerProcess firstFailure = RecordingServerProcess.exited(1);
        RecordingServerProcess secondFailure = RecordingServerProcess.exited(2);
        RecordingProcessLauncher launcher = new RecordingProcessLauncher(executable, firstFailure, secondFailure);
        MavsdkServerConfig config = new MavsdkServerConfig(
            executable,
            List.of("udp://127.0.0.1:14540"),
            Duration.ofMillis(1),
            Duration.ofMillis(2),
            1);

        try (MavsdkServerManager manager = new MavsdkServerManager(config, launcher)) {
            IOException error = assertThrows(IOException.class, manager::startServerProcess);

            assertTrue(error.getMessage().contains("exited with code 2"));
            assertEquals(DriverStatus.FAILED, manager.status());
            assertEquals(2, launcher.launchedCommands().size(), "initial launch plus one restart attempt must be bounded");
        }
    }

    @Test
    void unitRestartIfExitedRestartsPreviouslyConnectedServerAfterNonZeroExit() throws Exception {
        // @unit scenario: a CONNECTED driver restarts mavsdk_server after a non-zero process exit.
        Path executable = writeServerScript("fake-ready");
        RecordingServerProcess connectedThenExited = RecordingServerProcess.running();
        RecordingServerProcess restarted = RecordingServerProcess.running();
        RecordingProcessLauncher launcher = new RecordingProcessLauncher(executable, connectedThenExited, restarted);
        MavsdkServerConfig config = new MavsdkServerConfig(
            executable,
            List.of("udp://127.0.0.1:14540"),
            Duration.ofMillis(1),
            Duration.ofMillis(2),
            1);

        try (MavsdkServerManager manager = new MavsdkServerManager(config, launcher)) {
            manager.startServerProcess();
            connectedThenExited.exitWith(9);

            boolean restartAttempted = manager.restartIfExited();

            assertTrue(restartAttempted, "restartIfExited should restart after exit code 9");
            assertEquals(DriverStatus.CONNECTED, manager.status());
            assertEquals(2, launcher.launchedCommands().size());
            assertTrue(restarted.isAlive());
        }
    }

    @Test
    void startsMavsdkServerProcessAndStopsItCleanly() throws Exception {
        Path script = writeServerScript("ready");
        MavsdkServerConfig config = new MavsdkServerConfig(
            script,
            List.of("--system-address", "udp://:14540"),
            Duration.ofMillis(50),
            Duration.ofSeconds(2));

        try (MavsdkServerManager manager = new MavsdkServerManager(config)) {
            ManagedMavsdkServer server = manager.startServerProcess();

            assertTrue(server.isAlive(), "server process should be alive after startup");
            assertEquals(script, server.executable(), "managed process should report the executable it launched");
        }
    }

    @Test
    void rejectsUnsafeOrMissingMavsdkServerExecutable() {
        Path missing = Path.of("does-not-exist-mavsdk-server");

        IllegalArgumentException error = assertThrows(
            IllegalArgumentException.class,
            () -> new MavsdkServerConfig(missing, List.of(), Duration.ofMillis(10), Duration.ofMillis(50)));
        assertTrue(error.getMessage().contains("executable"));
    }

    @Test
    void connectsToMavlinkSystemAfterHeartbeatAndParsesSystemId() throws Exception {
        int port = freeUdpPort();
        MavlinkSystemConnector connector = new MavlinkSystemConnector(
            InetAddress.getLoopbackAddress().getHostAddress(),
            port,
            Duration.ofSeconds(2),
            20);
        Thread sender = new Thread(() -> sendHeartbeat(port, MavlinkHeartbeat.v1(42, 1)), "heartbeat-sender");
        sender.start();

        MavlinkSystemConnection connection = connector.awaitConnectedVehicle();

        sender.join(1_000);
        assertTrue(connection.connected(), "mavsdk_core_connected equivalent should be true after HEARTBEAT");
        assertEquals(42, connection.systemId());
        assertEquals(1, connection.componentId());
        assertArrayEquals(MavlinkHeartbeat.v1(42, 1), connection.rawHeartbeat());
    }

    @Test
    void mapsHeartbeatTelemetryToOgcDataStreamObservation() {
        // Requirement sitl-telemetry-and-control: telemetry/data-stream plugin path maps HEARTBEAT to OGC DataStreams.
        MavlinkSystemConnection connection = MavlinkHeartbeat.parse(MavlinkHeartbeat.v1(42, 1), 17).orElseThrow();

        OgcDataStreamObservation observation = OgcDataStreamMapper.fromHeartbeat("px4-heartbeat", connection);

        assertEquals("px4-heartbeat", observation.dataStreamId());
        assertEquals("HEARTBEAT", observation.propertyName());
        assertEquals(42, observation.result().get("systemId"));
        assertEquals(1, observation.result().get("componentId"));
        assertEquals(Boolean.TRUE, observation.result().get("mavsdk_core_connected"));
        assertNotNull(observation.phenomenonTime());
    }

    @Test
    void controlActionRequiresConnectedVehicleAndReturnsCommandResult() {
        // Requirement sitl-telemetry-and-control: control plugin path is health-gated on mavsdk_core_connected.
        MavlinkSystemConnection connection = MavlinkHeartbeat.parse(MavlinkHeartbeat.v1(7, 1), 17).orElseThrow();
        AtomicInteger executions = new AtomicInteger();
        MavsdkActionControl control = new MavsdkActionControl(connection, (connectedVehicle, request) -> {
            executions.incrementAndGet();
            return "MAVSDK HOLD accepted for system " + connectedVehicle.systemId();
        });

        CommandResult result = control.executeAction(CommandRequest.holdPosition("cmd-hold"));

        assertEquals("cmd-hold", result.commandId());
        assertEquals(CommandStatus.SUCCEEDED, result.status());
        assertTrue(result.message().contains("HOLD"));
        assertEquals(1, executions.get(), "HOLD must be delegated to the MAVSDK action executor");
    }

    @Test
    void rejectsControlActionWhenVehicleIsNotConnected() {
        IllegalArgumentException error = assertThrows(
            IllegalArgumentException.class,
            () -> new MavsdkActionControl(null, (connectedVehicle, request) -> "unused"));

        assertTrue(error.getMessage().contains("connected vehicle"));
    }

    @Test
    void returnsEmptyWhenNoHeartbeatArrivesBeforeBoundedAttemptsExpire() throws Exception {
        MavlinkSystemConnector connector = new MavlinkSystemConnector(
            InetAddress.getLoopbackAddress().getHostAddress(),
            freeUdpPort(),
            Duration.ofMillis(20),
            2);

        Optional<MavlinkSystemConnection> connection = connector.tryAwaitConnectedVehicle();

        assertFalse(connection.isPresent(), "connection should not be reported without HEARTBEAT");
    }

    @Test
    @Tag("integration")
    @Tag("mavlink.px4-sitl.mavsdk-smoke")
    void sitlProfileSmokeUsesHarnessEndpointAndReportsMavsdkJavaClientConnectedWithinThirtySeconds() throws Exception {
        // @integration scenario: sitl-telemetry-and-control. Harness profile evidence anchors:
        // mavlink.px4-sitl.mavsdk-smoke, px4io/px4-sitl, 14540, SITL_UDP_ENDPOINT,
        // PX4_SIM_MODEL, MAVSDK Java client, mavsdk_core_connected, CONNECTED, HEARTBEAT.
        // The qa-runner owns the PX4 SITL service; this tagged smoke consumes only the
        // rendered SITL_UDP_ENDPOINT and PX4_SIM_MODEL values without falling back to
        // localhost, a fixed port, or a locally started container.
        String px4SimModel = harnessEnv("PX4_SIM_MODEL");
        String sitlUdpEndpoint = harnessEnv("SITL_UDP_ENDPOINT");
        SitlUdpEndpoint endpoint = SitlUdpEndpoint.parse(sitlUdpEndpoint);
        MavsdkJavaClientConnectionProbe mavsdkJavaClient = new MavsdkJavaClientConnectionProbe(endpoint, Duration.ofSeconds(30));

        MavsdkJavaClientConnection clientConnection = mavsdkJavaClient.awaitConnected();
        OgcDataStreamObservation heartbeatObservation = OgcDataStreamMapper.fromHeartbeat(
            "px4-sitl-heartbeat",
            clientConnection.connection());
        MavsdkActionExecutor liveHoldExecutor = new MavlinkHoldActionExecutor(endpoint.host(), endpoint.port(), 250, 1);
        CommandResult commandResult = new MavsdkActionControl(clientConnection.connection(), liveHoldExecutor)
            .executeAction(CommandRequest.holdPosition("sitl-hold-position"));

        assertEquals("iris", px4SimModel, "PX4_SIM_MODEL should identify the harness SITL airframe under test");
        assertTrue(clientConnection.mavsdkJavaClientConnected(), "MAVSDK Java client should connect within 30 seconds");
        assertEquals(DriverStatus.CONNECTED, clientConnection.driverStatus(), "driver status should be CONNECTED within 30 seconds");
        assertTrue(clientConnection.connection().connected(), "mavsdk_core_connected should become true for PX4 SITL HEARTBEAT");
        assertTrue(clientConnection.connection().systemId() > 0, "PX4 SITL heartbeat should include a MAVLink system id");
        assertNotNull(clientConnection.connection().observedAt());
        assertEquals("HEARTBEAT", heartbeatObservation.propertyName(), "telemetry plugin path should map HEARTBEAT to DataStream");
        assertEquals(Boolean.TRUE, heartbeatObservation.result().get("mavsdk_core_connected"));
        assertEquals(CommandStatus.SUCCEEDED, commandResult.status(), "control plugin action should send a real MAVLink hold command after health gate");
    }

    private static String harnessEnv(String name) {
        String value = System.getenv(name);
        Assumptions.assumeTrue(
            value != null && !value.isBlank(),
            name + " is supplied by the mavlink.px4-sitl.mavsdk-smoke integration harness");
        return value;
    }

    private static int freeUdpPort() throws IOException {
        try (DatagramSocket socket = new DatagramSocket(0, InetAddress.getLoopbackAddress())) {
            return socket.getLocalPort();
        }
    }

    private static Path writeServerScript(String readyLine) throws IOException {
        Path script = Files.createTempFile("fake-mavsdk-server", ".sh");
        String content = "#!/bin/sh\n" +
            "echo '" + readyLine + "'\n" +
            "trap 'exit 0' TERM INT\n" +
            "while true; do sleep 1; done\n";
        Files.writeString(script, content, StandardCharsets.UTF_8);
        assertTrue(script.toFile().setExecutable(true), "test script should be executable");
        return script;
    }

    private static void sendHeartbeat(int port, byte[] heartbeat) {
        try (DatagramSocket sender = new DatagramSocket()) {
            Thread.sleep(100);
            DatagramPacket packet = new DatagramPacket(
                heartbeat,
                heartbeat.length,
                InetAddress.getLoopbackAddress(),
                port);
            sender.send(packet);
        } catch (IOException error) {
            throw new IllegalStateException("failed to send test heartbeat", error);
        } catch (InterruptedException error) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("heartbeat sender interrupted", error);
        }
    }

    private static final class RecordingProcessLauncher implements MavsdkServerManager.ProcessLauncher {
        private final Path executable;
        private final Queue<RecordingServerProcess> processes;
        private final List<List<String>> launchedCommands = new ArrayList<>();

        RecordingProcessLauncher(Path executable, RecordingServerProcess... processes) {
            this.executable = executable;
            this.processes = new ArrayDeque<>(List.of(processes));
        }

        @Override
        public ManagedMavsdkServer launch(List<String> command) throws IOException {
            if (processes.isEmpty()) {
                throw new IOException("no fake mavsdk_server process configured");
            }
            launchedCommands.add(List.copyOf(command));
            return new ManagedMavsdkServer(executable, processes.remove());
        }

        List<List<String>> launchedCommands() {
            return launchedCommands.stream().<List<String>>map(ArrayList::new).toList();
        }
    }

    private static final class RecordingServerProcess implements ManagedMavsdkServer.ServerProcess {
        private final AtomicBoolean alive;
        private OptionalInt exitCode;

        private RecordingServerProcess(boolean alive, OptionalInt exitCode) {
            this.alive = new AtomicBoolean(alive);
            this.exitCode = exitCode;
        }

        static RecordingServerProcess running() {
            return new RecordingServerProcess(true, OptionalInt.empty());
        }

        static RecordingServerProcess exited(int exitCode) {
            return new RecordingServerProcess(false, OptionalInt.of(exitCode));
        }

        void exitWith(int code) {
            alive.set(false);
            exitCode = OptionalInt.of(code);
        }

        @Override
        public boolean isAlive() {
            return alive.get();
        }

        @Override
        public OptionalInt exitCode() {
            return exitCode;
        }

        @Override
        public void close() {
            alive.set(false);
            if (exitCode.isEmpty()) {
                exitCode = OptionalInt.of(0);
            }
        }
    }
}
