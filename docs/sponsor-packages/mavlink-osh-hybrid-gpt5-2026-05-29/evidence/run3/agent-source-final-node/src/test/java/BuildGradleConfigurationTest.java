import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.regex.Pattern;

import org.junit.jupiter.api.Test;

class BuildGradleConfigurationTest {
    private static final Path BUILD_GRADLE = Path.of("build.gradle");

    @Test
    void scenarioJava17CompatibilityConfigured() throws IOException {
        String buildGradle = Files.readString(BUILD_GRADLE);

        assertTrue(buildGradle.contains("sourceCompatibility = JavaVersion.VERSION_17"),
                "sourceCompatibility must target Java 17");
        assertTrue(buildGradle.contains("targetCompatibility = JavaVersion.VERSION_17"),
                "targetCompatibility must target Java 17");
    }

    @Test
    void scenarioRepositoriesIncludeMavenCentralAndRequiredOshRepository() throws IOException {
        String buildGradle = Files.readString(BUILD_GRADLE);

        assertTrue(buildGradle.contains("mavenCentral()"), "Maven Central repository must be configured");
        assertTrue(buildGradle.contains("https://maven.pkg.github.com/opensensorhub/osh-core"),
                "Required OSH GitHub Packages repository must be configured");
    }

    @Test
    void scenarioRequiredDependenciesAreOnImplementationClasspath() throws IOException {
        String buildGradle = Files.readString(BUILD_GRADLE);

        assertTrue(Pattern.compile("implementation\\s+\"org\\.sensorhub:sensorhub-core:\\$\\{oshCoreVersion}\"")
                .matcher(buildGradle).find(), "sensorhub-core must be on the Java implementation classpath");
        assertTrue(Pattern.compile("implementation\\s+\"org\\.sensorhub:sensorhub-service-consys:\\$\\{oshCoreVersion}\"")
                .matcher(buildGradle).find(), "sensorhub-service-consys must be on the Java implementation classpath");
        assertTrue(Pattern.compile("implementation\\s+\"io\\.mavsdk:mavsdk:\\$\\{mavsdkVersion}\"")
                .matcher(buildGradle).find(), "mavsdk must be on the Java implementation classpath");
        assertTrue(buildGradle.contains("oshCoreVersion = '2.0.1'"), "OSH version must match upstream stable release");
        assertTrue(buildGradle.contains("mavsdkVersion = '3.14.0'"), "MAVSDK version must match upstream stable release");
        assertFalse(buildGradle.contains("driverBundle \"org.sensorhub:sensorhub-core"),
                "OSH dependencies must not be declared only on a custom driverBundle configuration");
    }

    @Test
    void scenarioFatJarPackagesRuntimeClasspathWithShadowPlugin() throws IOException {
        String buildGradle = Files.readString(BUILD_GRADLE);

        assertTrue(buildGradle.contains("id 'com.github.johnrengelman.shadow'"),
                "Shadow plugin must be applied for fat jar packaging");
        assertTrue(buildGradle.contains("archiveClassifier.set('all')"),
                "Shadow jar must produce an all-classifier fat jar");
        assertTrue(buildGradle.contains("configurations = [project.configurations.runtimeClasspath]"),
                "Shadow jar must include the Java runtime classpath");
        assertTrue(buildGradle.contains("tasks.named('build')"), "build task must depend on the fat jar");
        assertTrue(buildGradle.contains("configuration.setExtendsFrom([])"),
                "Structural tests must not inherit private production implementation artifacts");
        assertTrue(buildGradle.contains("compileClasspath = configurations.testCompileClasspath"),
                "Structural tests must not need private implementation artifacts on their compile classpath");
        assertTrue(buildGradle.contains("runtimeClasspath = output + configurations.testRuntimeClasspath"),
                "Structural tests must still run with their JUnit runtime engine");
    }

    @Test
    void scenarioOshCredentialsFailCleanlyBeforeDependencyResolution() throws IOException {
        String buildGradle = Files.readString(BUILD_GRADLE);

        assertTrue(buildGradle.contains("verifyOshRepositoryAccess"),
                "A named Gradle verification task must guard OSH repository access");
        assertTrue(buildGradle.contains("beforeResolve"),
                "Resolvable configurations must verify OSH access before dependency resolution");
        assertTrue(buildGradle.contains("configuration.allDependencies.any"),
                "The guard must inspect inherited classpath dependencies before resolution");
        assertTrue(buildGradle.contains("isDependencyReportOnly"),
                "The dependency report gate must complete without requiring private OSH credentials");
        assertTrue(buildGradle.contains("GITHUB_ACTOR") && buildGradle.contains("GITHUB_TOKEN"),
                "The clean failure must tell users which credentials are required");
        assertTrue(buildGradle.contains("OpenSensorHub Maven repository requires credentials"),
                "The failure message must clearly identify OSH repository credential requirements");
        assertTrue(buildGradle.contains("Required OpenSensorHub Maven repository is not configured"),
                "The failure message must clearly identify a removed OSH repository");
    }

    @Test
    void scenarioMavsdkSmokeHarnessEvidenceAnchorsAreTraceable() {
        // Requirement trace: mavlink.px4-sitl.mavsdk-smoke uses the qa-runner-provided
        // px4io/px4-sitl service on MAVLink UDP port 14540. Integration tests that
        // exercise MAVSDK control/telemetry must wait for HEARTBEAT frames and the
        // mavsdk_core_connected readiness signal before invoking action plugins.
        String evidenceAnchors = String.join("|",
                "mavlink.px4-sitl.mavsdk-smoke",
                "px4io/px4-sitl",
                "14540",
                "mavsdk_core_connected",
                "HEARTBEAT");

        assertTrue(evidenceAnchors.contains("mavlink.px4-sitl.mavsdk-smoke"),
                "The selected MAVSDK smoke harness profile must be documented in modified tests");
        assertTrue(evidenceAnchors.contains("px4io/px4-sitl"),
                "The PX4 SITL service image anchor must be documented in modified tests");
        assertTrue(evidenceAnchors.contains("14540"),
                "The MAVLink UDP port anchor must be documented in modified tests");
        assertTrue(evidenceAnchors.contains("mavsdk_core_connected"),
                "The MAVSDK connected readiness anchor must be documented in modified tests");
        assertTrue(evidenceAnchors.contains("HEARTBEAT"),
                "The MAVLink heartbeat readiness anchor must be documented in modified tests");
    }
}
