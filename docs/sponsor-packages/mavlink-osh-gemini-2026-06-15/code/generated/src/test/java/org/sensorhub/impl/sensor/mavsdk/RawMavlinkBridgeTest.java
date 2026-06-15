package org.sensorhub.impl.sensor.mavsdk;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.AfterEach;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import static org.junit.jupiter.api.Assertions.*;

public class RawMavlinkBridgeTest {
    
    @Mock
    private MavSdkDriver driver;
    
    private RawMavlinkBridge bridge;

    @BeforeEach
    public void setup() {
        MockitoAnnotations.openMocks(this);
    }

    @AfterEach
    public void teardown() {
        if (bridge != null) bridge.stop();
    }

    @Test
    public void testStartAndStop() throws Exception {
        bridge = new RawMavlinkBridge(driver, "udp://127.0.0.1:14555");
        bridge.start();
        assertTrue(bridge.isRunning());
        bridge.stop();
        assertFalse(bridge.isRunning());
    }

    @Test
    public void testCustomXmlDialectLogicAndRouting() throws Exception {
        bridge = new RawMavlinkBridge(driver, "udp://127.0.0.1:14556");
        bridge.start();
        
        // 1. Load Custom XML dialect
        String xml = "<?xml version=\"1.0\"?><mavlink><messages><message id=\"999\" name=\"CUSTOM_TEST\"><field type=\"uint8_t\" name=\"data\">Data</field></message></messages></mavlink>";
        bridge.loadXmlDialect(xml);
        
        assertTrue(bridge.getCustomDialect().containsKey(999));
        assertEquals("CUSTOM_TEST", bridge.getCustomDialect().get(999));

        // 2. Simulate raw MAVLink 1 packet
        // 0xFE, len(1), seq(0), sys(1), comp(1), msgId(999=0xE7 if byte, wait MAVLink1 is 1 byte so 0-255. Let's use id 200).
        // Let's reload dialect with id 200
        String xml2 = "<?xml version=\"1.0\"?><mavlink><messages><message id=\"200\" name=\"CUSTOM_TEST_200\"><field type=\"uint8_t\" name=\"data\">Data</field></message></messages></mavlink>";
        bridge.loadXmlDialect(xml2);

        byte[] mavlink1 = new byte[] {
            (byte)0xFE, // Magic
            1,          // payload length
            0,          // seq
            1,          // sysId
            1,          // compId
            (byte)200,  // msgId
            (byte)42,   // payload (1 byte)
            (byte)0xAA, (byte)0xBB // CRC
        };
        
        bridge.handleRawPacket(mavlink1, mavlink1.length);
        
        var routed = bridge.getRoutedMessages();
        assertEquals(1, routed.size());
        assertEquals(200, routed.get(0).msgId);
        assertEquals("CUSTOM_TEST_200", routed.get(0).name);
        assertEquals(1, routed.get(0).payload.length);
        assertEquals(42, routed.get(0).payload[0]);

        // 3. Simulate raw MAVLink 2 packet (msgId can be > 255)
        byte[] mavlink2 = new byte[] {
            (byte)0xFD, // Magic
            2,          // payload len
            0,          // inc flags
            0,          // cmp flags
            0,          // seq
            1,          // sysid
            1,          // compid
            (byte)0xE7, (byte)0x03, (byte)0x00, // msgId = 999
            (byte)10, (byte)20, // payload
            (byte)0xCC, (byte)0xDD // CRC
        };

        bridge.handleRawPacket(mavlink2, mavlink2.length);

        routed = bridge.getRoutedMessages();
        assertEquals(2, routed.size());
        assertEquals(999, routed.get(1).msgId);
        assertEquals("CUSTOM_TEST", routed.get(1).name);
        assertEquals(2, routed.get(1).payload.length);
        assertEquals(10, routed.get(1).payload[0]);
        assertEquals(20, routed.get(1).payload[1]);
    }
}
