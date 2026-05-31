import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import org.gradle.testkit.runner.BuildResult;
import org.gradle.testkit.runner.GradleRunner;
import org.gradle.testkit.runner.TaskOutcome;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class CoverageMatrixGradleTaskTest {
    // HARNESS-PROFILE: mavlink.px4-sitl.mavsdk-smoke uses px4io/px4-sitl UDP 14540; readiness requires mavsdk_core_connected and HEARTBEAT before MAVSDK plugin assertions.
    @TempDir
    Path projectDir;

    // REQ-COVERAGE-MATRIX-GRADLE: checkCoverageMatrix accepts the machine-checkable README matrix.
    @Test
    void checkCoverageMatrixTaskScenario_acceptsRequiredRuntimeMappingsAndDeferredRationales() throws IOException {
        copyBuildLogicToTemporaryProject();
        writeReadme("""
                # Fixture

                | MAVSDK plugin | MAVLink evidence handled | CS API type | Runtime mapping | Rationale |
                | --- | --- | --- | --- | --- |
                | Telemetry | `HEARTBEAT` | DataStream status | Runtime | Keeps DataStream online status aligned with vehicle liveness. |
                | Telemetry | `GLOBAL_POSITION_INT` GPS | Observation | Runtime | Emits position and altitude as CS API observations. |
                | Action | `MAV_CMD_COMPONENT_ARM_DISARM` | ControlStream | Runtime | Covers the arm command through MAVSDK Action acknowledgement. |
                | Mission | `MISSION_ITEM_INT` | Deferred | Deferred | Mission upload needs transactional task semantics before exposure. |
                """);

        BuildResult result = runGradle("checkCoverageMatrix");

        assertEquals(TaskOutcome.SUCCESS, result.task(":checkCoverageMatrix").getOutcome());
    }

    // REQ-COVERAGE-MATRIX-GRADLE: Telemetry runtime mappings must include Observation GPS and HEARTBEAT status coverage.
    @Test
    void checkCoverageMatrixTaskScenario_rejectsMissingTelemetryObservationMapping() throws IOException {
        copyBuildLogicToTemporaryProject();
        writeReadme("""
                # Fixture

                | MAVSDK plugin | MAVLink evidence handled | CS API type | Runtime mapping | Rationale |
                | --- | --- | --- | --- | --- |
                | Telemetry | `HEARTBEAT` | DataStream status | Runtime | Keeps DataStream online status aligned with vehicle liveness. |
                | Action | `MAV_CMD_COMPONENT_ARM_DISARM` | ControlStream | Runtime | Covers the arm command through MAVSDK Action acknowledgement. |
                """);

        BuildResult result = runGradleAndFail("checkCoverageMatrix");

        assertTrue(result.getOutput().contains("Telemetry Observation stream"));
    }

    // REQ-COVERAGE-MATRIX-GRADLE: Action runtime mappings must include the arm ControlStream command.
    @Test
    void checkCoverageMatrixTaskScenario_rejectsMissingActionArmControlStreamMapping() throws IOException {
        copyBuildLogicToTemporaryProject();
        writeReadme("""
                # Fixture

                | MAVSDK plugin | MAVLink evidence handled | CS API type | Runtime mapping | Rationale |
                | --- | --- | --- | --- | --- |
                | Telemetry | `HEARTBEAT` | DataStream status | Runtime | Keeps DataStream online status aligned with vehicle liveness. |
                | Telemetry | `GLOBAL_POSITION_INT` GPS | Observation | Runtime | Emits position and altitude as CS API observations. |
                | Action | `MAV_CMD_NAV_TAKEOFF` | ControlStream | Deferred | Takeoff requires health-gated SITL acceptance first. |
                """);

        BuildResult result = runGradleAndFail("checkCoverageMatrix");

        assertTrue(result.getOutput().contains("Action ControlStream mapping for the 'arm' command"));
    }

    // REQ-COVERAGE-MATRIX-GRADLE: every Deferred cell must be accompanied by a rationale.
    @Test
    void checkCoverageMatrixTaskScenario_rejectsDeferredMappingWithoutRationale() throws IOException {
        copyBuildLogicToTemporaryProject();
        writeReadme("""
                # Fixture

                | MAVSDK plugin | MAVLink evidence handled | CS API type | Runtime mapping | Rationale |
                | --- | --- | --- | --- | --- |
                | Telemetry | `HEARTBEAT` | DataStream status | Runtime | Keeps DataStream online status aligned with vehicle liveness. |
                | Telemetry | `GLOBAL_POSITION_INT` GPS | Observation | Runtime | Emits position and altitude as CS API observations. |
                | Action | `MAV_CMD_COMPONENT_ARM_DISARM` | ControlStream | Runtime | Covers the arm command through MAVSDK Action acknowledgement. |
                | Offboard | `SET_POSITION_TARGET_LOCAL_NED` | Deferred | Deferred | |
                """);

        BuildResult result = runGradleAndFail("checkCoverageMatrix");

        assertTrue(result.getOutput().contains("Deferred entries must include a rationale"));
    }

    private void copyBuildLogicToTemporaryProject() throws IOException {
        Files.copy(Path.of("build.gradle"), projectDir.resolve("build.gradle"));
        Files.writeString(projectDir.resolve("settings.gradle"), "rootProject.name = 'coverage-matrix-fixture'\n");
    }

    private void writeReadme(String content) throws IOException {
        Files.writeString(projectDir.resolve("README.md"), content);
    }

    private BuildResult runGradle(String... arguments) {
        return gradleRunner(arguments).build();
    }

    private BuildResult runGradleAndFail(String... arguments) {
        return gradleRunner(arguments).buildAndFail();
    }

    private GradleRunner gradleRunner(String... arguments) {
        return GradleRunner.create()
                .withProjectDir(projectDir.toFile())
                .withArguments(arguments)
                .forwardOutput();
    }
}
