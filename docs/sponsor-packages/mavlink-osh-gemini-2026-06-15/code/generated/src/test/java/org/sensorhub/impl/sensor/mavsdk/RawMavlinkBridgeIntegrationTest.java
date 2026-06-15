package org.sensorhub.impl.sensor.mavsdk;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Tag;
import static org.junit.jupiter.api.Assertions.*;
import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.InetAddress;

@Tag("integration")
public class RawMavlinkBridgeIntegrationTest {

    @Test
    public void testRawMavlinkFallbackIntegration() throws Exception {
        MavSdkDriver driver = new MavSdkDriver();
        UnmannedConfig config = new UnmannedConfig();
        config.connectionString = "udp://127.0.0.1:14555";
        driver.init(config);
        
        RawMavlinkBridge bridge = new RawMavlinkBridge(driver, config.connectionString);
        bridge.start();
        assertTrue(bridge.isRunning());
        
        // Load an XML dialect
        String xml = "<?xml version=\"1.0\"?><mavlink><messages><message id=\"50\" name=\"INTEGRATION_TEST\"><field type=\"uint8_t\" name=\"data\">Data</field></message></messages></mavlink>";
        bridge.loadXmlDialect(xml);

        // Send a UDP packet representing a raw MAVLink 1 message (msgId = 50)
        try (DatagramSocket socket = new DatagramSocket()) {
            byte[] mavlink1 = new byte[] {
                (byte)0xFE, // Magic
                1,          // payload length
                0,          // seq
                1,          // sysId
                1,          // compId
                (byte)50,   // msgId
                (byte)99,   // payload (1 byte)
                (byte)0xAA, (byte)0xBB // CRC
            };
            DatagramPacket packet = new DatagramPacket(mavlink1, mavlink1.length, InetAddress.getByName("127.0.0.1"), 14555);
            socket.send(packet);
            
            // Wait to allow processing
            Thread.sleep(500);
        }
        
        var routed = bridge.getRoutedMessages();
        assertFalse(routed.isEmpty(), "Bridge should have routed the UDP packet");
        assertEquals(50, routed.get(0).msgId);
        assertEquals("INTEGRATION_TEST", routed.get(0).name);
        assertEquals(99, routed.get(0).payload[0]);

        bridge.stop();
        assertFalse(bridge.isRunning());
    }
}
