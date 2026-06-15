package org.sensorhub.impl.sensor.mavsdk;

import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;
import org.sensorhub.api.common.SensorHubException;

public class MavSdkDriverTest {

    @Test
    public void testScenario_1_1_1_Init() throws Exception {
        MavSdkDriver driver = new MavSdkDriver();
        UnmannedConfig config = new UnmannedConfig();
        config.connectionString = "udp://127.0.0.1:14540";
        driver.init(config);
        
        assertNotNull(driver.getServerHandler());
        assertEquals("udp://127.0.0.1:14540", driver.getServerHandler().getConnectionString());
        assertTrue(driver.isInitialized());
        assertFalse(driver.isStarted());
    }

    @Test
    public void testScenario_1_1_2_StartFails() throws Exception {
        MavSdkDriver driver = new MavSdkDriver();
        UnmannedConfig config = new UnmannedConfig();
        config.connectionString = "udp://127.0.0.1:14540";
        driver.init(config);
        
        // Mock the handler to fail
        driver.getServerHandler().setCustomBinaryPath("/non/existent/path");
        
        assertThrows(SensorHubException.class, () -> {
            driver.doStart();
        });
        
        assertFalse(driver.isStarted());
    }
}
