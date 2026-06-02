package org.sensorhub.impl.sensor.mavsdk;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertIterableEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.List;
import java.util.OptionalInt;
import java.util.Queue;
import java.util.concurrent.atomic.AtomicBoolean;
import org.junit.jupiter.api.Test;

class MavsdkServerLifecycleTest {
    @Test
    void unitStartsMavsdkServerWithConfiguredExecutableAndShutsItDown() throws IOException {
        // @unit scenario: mavsdk_server lifecycle uses fakes/in-process state to verify executable path.
        RecordingProcessLauncher launcher = new RecordingProcessLauncher(
                RecordingManagedProcess.running());
        MavsdkServerLifecycle lifecycle = new MavsdkServerLifecycle(
                launcher,
                "/opt/mavsdk/bin/mavsdk_server",
                "udp://sitl.example.test:14540",
                50_051,
                1);

        lifecycle.start();

        assertEquals(MavsdkServerLifecycle.DriverStatus.CONNECTED, lifecycle.status());
        assertTrue(lifecycle.isRunning(), "mavsdk_server should report running after start");
        assertIterableEquals(
                List.of("/opt/mavsdk/bin/mavsdk_server", "-p", "50051", "udp://sitl.example.test:14540"),
                launcher.launchedCommands().get(0),
                "mavsdk_server must launch the configured executable and target SITL UDP endpoint");

        lifecycle.shutdown();

        assertEquals(MavsdkServerLifecycle.DriverStatus.STOPPED, lifecycle.status());
        assertFalse(launcher.lastProcessIsAlive(), "shutdown must terminate the mavsdk_server process");
    }

    @Test
    void unitRetriesStartupWhenMavsdkServerExitsWithNonZeroStatus() throws IOException {
        // @unit scenario: non-zero mavsdk_server process exit triggers a bounded restart attempt.
        RecordingManagedProcess failed = RecordingManagedProcess.exited(2);
        RecordingManagedProcess restarted = RecordingManagedProcess.running();
        RecordingProcessLauncher launcher = new RecordingProcessLauncher(failed, restarted);
        MavsdkServerLifecycle lifecycle = new MavsdkServerLifecycle(
                launcher,
                "mavsdk_server",
                "udp://127.0.0.1:14540",
                50_051,
                1);

        lifecycle.start();

        assertEquals(MavsdkServerLifecycle.DriverStatus.CONNECTED, lifecycle.status());
        assertEquals(2, launcher.launchedCommands().size(), "one retry should restart mavsdk_server after exit code 2");
        assertFalse(failed.isAlive(), "failed process remains stopped in fake in-process state");
        assertTrue(restarted.isAlive(), "replacement process should be running after retry");
    }

    @Test
    void unitReportsFailedStatusWhenRestartBudgetIsExhausted() {
        // @unit scenario: driver status model records FAILED when all restart attempts exit non-zero.
        RecordingProcessLauncher launcher = new RecordingProcessLauncher(
                RecordingManagedProcess.exited(1),
                RecordingManagedProcess.exited(2));
        MavsdkServerLifecycle lifecycle = new MavsdkServerLifecycle(
                launcher,
                "mavsdk_server",
                "udp://127.0.0.1:14540",
                50_051,
                1);

        IOException error = assertThrows(IOException.class, lifecycle::start);

        assertTrue(error.getMessage().contains("exited with code 2"));
        assertEquals(MavsdkServerLifecycle.DriverStatus.FAILED, lifecycle.status());
        assertEquals(2, launcher.launchedCommands().size(), "initial launch plus one restart attempt must be bounded");
    }

    @Test
    void unitRestartIfExitedRestartsAPreviouslyConnectedServer() throws IOException {
        // @unit scenario: non-zero exit after CONNECTED status attempts a mavsdk_server restart.
        RecordingManagedProcess connectedThenExited = RecordingManagedProcess.running();
        RecordingManagedProcess restarted = RecordingManagedProcess.running();
        RecordingProcessLauncher launcher = new RecordingProcessLauncher(connectedThenExited, restarted);
        MavsdkServerLifecycle lifecycle = new MavsdkServerLifecycle(
                launcher,
                "mavsdk_server",
                "udp://127.0.0.1:14540",
                50_051,
                1);
        lifecycle.start();
        connectedThenExited.exitWith(9);

        boolean restartedAfterExit = lifecycle.restartIfExited();

        assertTrue(restartedAfterExit, "restartIfExited should restart after exit code 9");
        assertEquals(MavsdkServerLifecycle.DriverStatus.CONNECTED, lifecycle.status());
        assertEquals(2, launcher.launchedCommands().size());
        assertTrue(restarted.isAlive());
    }

    @Test
    void integrationProfileShutdownExposesShuttingDownStatusBeforeProcessIsStopped() throws IOException {
        // @integration scenario: mavlink.px4-sitl.mavsdk-smoke uses the qa-runner px4io/px4-sitl service.
        // Required evidence anchors: mavsdk_core_connected, HEARTBEAT, mavlink-udp/14540/udp.
        String px4SimModel = System.getenv("PX4_SIM_MODEL");
        String sitlEndpointFromProfile = "udp://" + px4SimModel + ".sitl.profile.invalid:14540";
        MavsdkServerLifecycle[] lifecycleRef = new MavsdkServerLifecycle[1];
        RecordingManagedProcess runningServer = RecordingManagedProcess.running();
        runningServer.onClose(() -> assertEquals(
                "SHUTTING_DOWN",
                lifecycleRef[0].status().name(),
                "shutdown must expose an in-progress state before closing mavsdk_server"));
        RecordingProcessLauncher launcher = new RecordingProcessLauncher(runningServer);
        MavsdkServerLifecycle lifecycle = new MavsdkServerLifecycle(
                launcher,
                "mavsdk_server",
                sitlEndpointFromProfile,
                50_051,
                0);
        lifecycleRef[0] = lifecycle;

        lifecycle.start();
        lifecycle.shutdown();

        assertEquals(MavsdkServerLifecycle.DriverStatus.STOPPED, lifecycle.status());
        assertFalse(runningServer.isAlive(), "shutdown must stop the profile-backed mavsdk_server process");
    }

    private static final class RecordingProcessLauncher implements MavsdkServerLifecycle.ProcessLauncher {
        private final Queue<RecordingManagedProcess> processes;
        private final List<List<String>> launchedCommands = new ArrayList<>();
        private RecordingManagedProcess lastProcess;

        RecordingProcessLauncher(RecordingManagedProcess... processes) {
            this.processes = new ArrayDeque<>(List.of(processes));
        }

        @Override
        public MavsdkServerLifecycle.ManagedProcess launch(List<String> command) throws IOException {
            if (processes.isEmpty()) {
                throw new IOException("no fake mavsdk_server process configured");
            }
            launchedCommands.add(List.copyOf(command));
            lastProcess = processes.remove();
            return lastProcess;
        }

        List<List<String>> launchedCommands() {
            return launchedCommands.stream().<List<String>>map(ArrayList::new).toList();
        }

        boolean lastProcessIsAlive() {
            return lastProcess != null && lastProcess.isAlive();
        }
    }

    private static final class RecordingManagedProcess implements MavsdkServerLifecycle.ManagedProcess {
        private final AtomicBoolean alive;
        private OptionalInt exitCode;
        private Runnable closeObserver = () -> { };

        private RecordingManagedProcess(boolean alive, OptionalInt exitCode) {
            this.alive = new AtomicBoolean(alive);
            this.exitCode = exitCode;
        }

        static RecordingManagedProcess running() {
            return new RecordingManagedProcess(true, OptionalInt.empty());
        }

        static RecordingManagedProcess exited(int exitCode) {
            return new RecordingManagedProcess(false, OptionalInt.of(exitCode));
        }

        void exitWith(int code) {
            alive.set(false);
            exitCode = OptionalInt.of(code);
        }

        void onClose(Runnable closeObserver) {
            this.closeObserver = closeObserver;
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
            closeObserver.run();
            alive.set(false);
            if (exitCode.isEmpty()) {
                exitCode = OptionalInt.of(0);
            }
        }
    }
}
