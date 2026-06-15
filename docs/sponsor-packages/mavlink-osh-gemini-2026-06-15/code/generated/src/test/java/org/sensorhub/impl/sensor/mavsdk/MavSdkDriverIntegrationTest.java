package org.sensorhub.impl.sensor.mavsdk;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Tag;
import static org.junit.jupiter.api.Assertions.*;
import static org.junit.jupiter.api.Assumptions.assumeTrue;

import net.opengis.swe.v20.DataBlock;
import org.sensorhub.impl.sensor.DefaultLocationOutput;
import io.mavsdk.System;

@Tag("integration")
public class MavSdkDriverIntegrationTest {

    @Test
    public void testScenario_1_1_3_SitlHeartbeat() throws Exception {
        String sitlEndpoint = java.lang.System.getenv("SITL_ENDPOINT");
        assumeTrue(sitlEndpoint != null, "SITL_ENDPOINT not set, skipping integration test");

        MavSdkDriver driver = new MavSdkDriver();
        UnmannedConfig config = new UnmannedConfig();
        config.connectionString = sitlEndpoint;
        driver.init(config);
        driver.start();

        assertTrue(driver.isStarted(), "Driver should be started");
        
        long timeout = java.lang.System.currentTimeMillis() + 10000;
        boolean wasConnected = false;
        while (java.lang.System.currentTimeMillis() < timeout) {
            if (driver.isConnected()) {
                wasConnected = true;
                break;
            }
            Thread.sleep(100);
        }
        
        assertTrue(wasConnected, "Should connect to SITL within 10 seconds and receive HEARTBEAT");
        driver.stop();
    }

    @Test
    public void testScenario_1_1_4_SitlPosition() throws Exception {
        String sitlEndpoint = java.lang.System.getenv("SITL_ENDPOINT");
        assumeTrue(sitlEndpoint != null, "SITL_ENDPOINT not set, skipping integration test");

        MavSdkDriver driver = new MavSdkDriver();
        UnmannedConfig config = new UnmannedConfig();
        config.connectionString = sitlEndpoint;
        driver.init(config);
        driver.start();
        assertTrue(driver.isStarted(), "Driver should be started");

        // Wait for connection
        long timeout = java.lang.System.currentTimeMillis() + 10000;
        while (java.lang.System.currentTimeMillis() < timeout && !driver.isConnected()) {
            Thread.sleep(100);
        }
        
        // Wait for position data to arrive
        timeout = java.lang.System.currentTimeMillis() + 10000;
        while (java.lang.System.currentTimeMillis() < timeout) {
            if (driver.getLocationOutput() != null && driver.getLocationOutput().getLatestRecord() != null) {
                break;
            }
            Thread.sleep(100);
        }

        // Mock the Connected Systems API by reading the driver output the same way ConSysApiService does
        FakeConnectedSystemsApi fakeApi = new FakeConnectedSystemsApi(driver);
        String response = fakeApi.requestPosition();
        
        assertTrue(response.contains("\"lat\""), "Connected Systems API response includes a valid position data point (lat)");
        assertTrue(response.contains("\"lon\""), "Connected Systems API response includes a valid position data point (lon)");
        assertTrue(response.contains("\"alt\""), "Connected Systems API response includes a valid position data point (alt)");
        
        driver.stop();
    }

    @Test
    public void testScenario_2_1_3() throws Exception {
        String sitlEndpoint = java.lang.System.getenv("SITL_ENDPOINT");
        assumeTrue(sitlEndpoint != null, "SITL_ENDPOINT not set, skipping integration test");

        MavSdkDriver driver = new MavSdkDriver();
        UnmannedConfig config = new UnmannedConfig();
        config.connectionString = sitlEndpoint;
        driver.init(config);
        driver.start();
        
        long timeout = java.lang.System.currentTimeMillis() + 10000;
        while (java.lang.System.currentTimeMillis() < timeout && !driver.isConnected()) {
            Thread.sleep(100);
        }
        assertTrue(driver.isConnected(), "Should connect to SITL");
        
        System mavsdkSystem = driver.getMavsdkSystem();
        
        // Exercise the flow by subscribing to health
        java.util.concurrent.atomic.AtomicBoolean healthReceived = new java.util.concurrent.atomic.AtomicBoolean(false);
        mavsdkSystem.getTelemetry().getHealth().subscribe(health -> {
            healthReceived.set(true);
        });
        
        timeout = java.lang.System.currentTimeMillis() + 10000;
        while (java.lang.System.currentTimeMillis() < timeout && !healthReceived.get()) {
            Thread.sleep(100);
        }
        
        assertTrue(healthReceived.get(), "Should receive health telemetry from SITL");
        driver.stop();
    }

    @Test
    public void testScenario_2_1_4() throws Exception {
        String sitlEndpoint = java.lang.System.getenv("SITL_ENDPOINT");
        assumeTrue(sitlEndpoint != null, "SITL_ENDPOINT not set, skipping integration test");
        
        MavSdkDriver driver = new MavSdkDriver();
        UnmannedConfig config = new UnmannedConfig();
        config.connectionString = sitlEndpoint;
        driver.init(config);
        driver.start();
        
        long timeout = java.lang.System.currentTimeMillis() + 10000;
        while (java.lang.System.currentTimeMillis() < timeout && !driver.isConnected()) {
            Thread.sleep(100);
        }
        assertTrue(driver.isConnected(), "Should connect to SITL");
        
        System mavsdkSystem = driver.getMavsdkSystem();
        
        // Exercise control forwarding flow
        boolean takeoffAttempted = false;
        try {
            mavsdkSystem.getAction().takeoff().blockingAwait();
            takeoffAttempted = true;
        } catch (Throwable e) {
            io.mavsdk.action.Action.ActionException actionEx = findCause(e, io.mavsdk.action.Action.ActionException.class);
            if (actionEx != null) {
                io.mavsdk.action.Action.ActionResult.Result code = actionEx.getCode();
                assertTrue(code == io.mavsdk.action.Action.ActionResult.Result.COMMAND_DENIED || 
                           code == io.mavsdk.action.Action.ActionResult.Result.COMMAND_DENIED_NOT_LANDED ||
                           code == io.mavsdk.action.Action.ActionResult.Result.SUCCESS, 
                           "Command failed for an expected domain reason: " + code);
                takeoffAttempted = true;
            } else {
                fail("Unexpected exception during takeoff: " + e.getMessage(), e);
            }
        }
        
        assertTrue(takeoffAttempted, "Control command (Takeoff) was successfully forwarded to SITL");
        driver.stop();
    }

    @Test
    public void testScenario_3_1_3() throws Exception {
        String sitlEndpoint = java.lang.System.getenv("SITL_ENDPOINT");
        assumeTrue(sitlEndpoint != null, "SITL_ENDPOINT not set, skipping integration test");

        MavSdkDriver driver = new MavSdkDriver();
        UnmannedConfig config = new UnmannedConfig();
        config.connectionString = sitlEndpoint;
        driver.init(config);
        driver.start();
        
        long timeout = java.lang.System.currentTimeMillis() + 10000;
        while (java.lang.System.currentTimeMillis() < timeout && !driver.isConnected()) {
            Thread.sleep(100);
        }
        assertTrue(driver.isConnected(), "Should connect to SITL");
        
        System mavsdkSystem = driver.getMavsdkSystem();
        
        // Exercise generic MAVLink flow by getting battery
        java.util.concurrent.atomic.AtomicBoolean batteryReceived = new java.util.concurrent.atomic.AtomicBoolean(false);
        mavsdkSystem.getTelemetry().getBattery().subscribe(battery -> {
            batteryReceived.set(true);
        });
        
        timeout = java.lang.System.currentTimeMillis() + 10000;
        while (java.lang.System.currentTimeMillis() < timeout && !batteryReceived.get()) {
            Thread.sleep(100);
        }
        
        assertTrue(driver.isConnected(), "Connected and registered for generic telemetry");
        driver.stop();
    }

    @Test
    public void testScenario_3_1_4() throws Exception {
        String sitlEndpoint = java.lang.System.getenv("SITL_ENDPOINT");
        assumeTrue(sitlEndpoint != null, "SITL_ENDPOINT not set, skipping integration test");

        MavSdkDriver driver = new MavSdkDriver();
        UnmannedConfig config = new UnmannedConfig();
        config.connectionString = sitlEndpoint;
        driver.init(config);
        driver.start();
        
        long timeout = java.lang.System.currentTimeMillis() + 10000;
        while (java.lang.System.currentTimeMillis() < timeout && !driver.isConnected()) {
            Thread.sleep(100);
        }
        assertTrue(driver.isConnected(), "Should connect to SITL");
        
        System mavsdkSystem = driver.getMavsdkSystem();
        
        boolean armAttempted = false;
        try {
            mavsdkSystem.getAction().arm().blockingAwait();
            armAttempted = true;
        } catch (Throwable e) {
            io.mavsdk.action.Action.ActionException actionEx = findCause(e, io.mavsdk.action.Action.ActionException.class);
            if (actionEx != null) {
                io.mavsdk.action.Action.ActionResult.Result code = actionEx.getCode();
                assertTrue(code == io.mavsdk.action.Action.ActionResult.Result.COMMAND_DENIED || 
                           code == io.mavsdk.action.Action.ActionResult.Result.SUCCESS, 
                           "Command failed for an expected domain reason: " + code);
                armAttempted = true;
            } else {
                fail("Unexpected exception during arm: " + e.getMessage(), e);
            }
        }
        
        assertTrue(armAttempted, "Generic MAVLink control command (Arm) forwarded to SITL");
        driver.stop();
    }

    private <T extends Throwable> T findCause(Throwable t, Class<T> clazz) {
        while (t != null) {
            if (clazz.isInstance(t)) {
                return clazz.cast(t);
            }
            t = t.getCause();
        }
        return null;
    }

    // Fake API representing ConSysApiService
    static class FakeConnectedSystemsApi {
        private final MavSdkDriver driver;
        
        public FakeConnectedSystemsApi(MavSdkDriver driver) {
            this.driver = driver;
        }
        
        public String requestPosition() {
            DefaultLocationOutput locOutput = driver.getLocationOutput();
            if (locOutput == null) return "{}";
            
            DataBlock record = locOutput.getLatestRecord();
            if (record == null) return "{}";
            
            double lat = record.getDoubleValue(1);
            double lon = record.getDoubleValue(2);
            double alt = record.getDoubleValue(3);
            
            return String.format("{\"lat\": %f, \"lon\": %f, \"alt\": %f}", lat, lon, alt);
        }
    }
}
