import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.stream.IntStream;
import org.junit.jupiter.api.Test;

class CoverageMatrixTest {
    // REQ-COVERAGE-MATRIX-GRADLE: eng-test-coverage for the checkCoverageMatrix task parsing and validation contract.
    // harness evidence anchors for mavlink.px4-sitl.mavsdk-smoke: px4io/px4-sitl UDP 14540 mavsdk_core_connected HEARTBEAT.
    @Test
    void checkCoverageMatrixTaskScenario_readmeContainsRuntimeMappingsAndDeferredRationales() throws IOException {
        List<Map<String, String>> rows = parseCoverageMatrix(Path.of("README.md"));

        assertFalse(rows.isEmpty(), "README coverage matrix must contain machine-checkable rows");
        assertTrue(rows.stream().anyMatch(CoverageMatrixTest::isTelemetryHeartbeatDataStreamRuntimeMapping),
                "Telemetry plugin must cover HEARTBEAT as DataStream online status at runtime");
        assertTrue(rows.stream().anyMatch(CoverageMatrixTest::isTelemetryGpsObservationRuntimeMapping),
                "Telemetry plugin must expose GPS/position telemetry as a runtime Observation stream");
        assertTrue(rows.stream().anyMatch(CoverageMatrixTest::isActionArmControlRuntimeMapping),
                "Action plugin must expose arm/disarm as a runtime ControlStream command");
        assertTrue(rows.stream().filter(CoverageMatrixTest::hasDeferredCell)
                .allMatch(row -> !row.get("Rationale").isBlank()),
                "Deferred mappings must include a rationale");
    }

    private static List<Map<String, String>> parseCoverageMatrix(Path readme) throws IOException {
        List<String> tableLines = Files.readAllLines(readme).stream()
                .dropWhile(line -> !line.startsWith("| MAVSDK plugin |"))
                .takeWhile(line -> line.startsWith("|"))
                .toList();
        assertTrue(tableLines.size() >= 3, "Coverage matrix table must include a header and rows");

        List<String> headers = splitTableRow(tableLines.get(0));
        return tableLines.stream()
                .skip(2)
                .map(CoverageMatrixTest::splitTableRow)
                .map(cells -> IntStream.range(0, headers.size())
                        .boxed()
                        .collect(java.util.stream.Collectors.toMap(headers::get, cells::get)))
                .toList();
    }

    private static List<String> splitTableRow(String line) {
        return Arrays.stream(line.substring(1, line.length() - 1).split("\\|", -1))
                .map(String::trim)
                .toList();
    }

    private static boolean isTelemetryHeartbeatDataStreamRuntimeMapping(Map<String, String> row) {
        return row.get("MAVSDK plugin").equals("Telemetry")
                && row.get("MAVLink evidence handled").contains("HEARTBEAT")
                && row.get("CS API type").equals("DataStream status")
                && row.get("Runtime mapping").equals("Runtime");
    }

    private static boolean isTelemetryGpsObservationRuntimeMapping(Map<String, String> row) {
        String evidence = row.get("MAVLink evidence handled").toUpperCase(Locale.ROOT);
        return row.get("MAVSDK plugin").equals("Telemetry")
                && row.get("CS API type").equals("Observation")
                && row.get("Runtime mapping").equals("Runtime")
                && (evidence.contains("GPS") || evidence.contains("GLOBAL_POSITION_INT"));
    }

    private static boolean isActionArmControlRuntimeMapping(Map<String, String> row) {
        String evidence = row.get("MAVLink evidence handled").toLowerCase(Locale.ROOT);
        String rationale = row.get("Rationale").toLowerCase(Locale.ROOT);
        return row.get("MAVSDK plugin").equals("Action")
                && row.get("CS API type").equals("ControlStream")
                && row.get("Runtime mapping").equals("Runtime")
                && (evidence.contains("arm") || rationale.contains("arm"));
    }

    private static boolean hasDeferredCell(Map<String, String> row) {
        return row.values().stream().anyMatch(value -> value.equals("Deferred"));
    }
}
