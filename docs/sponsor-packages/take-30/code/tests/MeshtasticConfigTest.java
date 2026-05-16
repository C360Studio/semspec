/*
 * Tests for MeshtasticConfig.
 *
 * Acceptance criteria:
 *   - Config has correct default values (scenario: config-defaults)
 *   - validate() throws on blank address (scenario: config-blank-address)
 *   - validate() throws on port 0 (scenario: config-port-zero)
 *   - validate() throws on port > 65535 (scenario: config-port-overflow)
 *   - validate() throws on negative reconnect delay (scenario: config-negative-delay)
 *   - validate() passes for valid config (scenario: config-valid)
 */

package org.sensorhub.impl.sensor.meshtastic;

import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.*;

/**
 * Unit tests for {@link MeshtasticConfig}.
 */
class MeshtasticConfigTest {

    // --- scenario: config-defaults ---

    @Test
    void defaultTcpAddressIsLocalhost() {
        MeshtasticConfig cfg = new MeshtasticConfig();
        assertEquals("localhost", cfg.tcpAddress,
                "Default TCP address should be 'localhost'");
    }

    @Test
    void defaultTcpPortIs4403() {
        MeshtasticConfig cfg = new MeshtasticConfig();
        assertEquals(MeshtasticConfig.DEFAULT_TCP_PORT, cfg.tcpPort,
                "Default TCP port should be 4403");
    }

    @Test
    void defaultReconnectDelayIs5000() {
        MeshtasticConfig cfg = new MeshtasticConfig();
        assertEquals(MeshtasticConfig.DEFAULT_RECONNECT_DELAY_MS, cfg.reconnectDelayMs,
                "Default reconnect delay should be 5000 ms");
    }

    @Test
    void defaultSensorUidIsNull() {
        MeshtasticConfig cfg = new MeshtasticConfig();
        assertNull(cfg.sensorUID, "sensorUID should be null by default");
    }

    // --- scenario: config-blank-address ---

    @Test
    void validateRejectsBlankTcpAddress() {
        MeshtasticConfig cfg = validConfig();
        cfg.tcpAddress = "   ";
        assertThrows(IllegalArgumentException.class, cfg::validate,
                "validate() should throw for a blank tcpAddress");
    }

    @Test
    void validateRejectsNullTcpAddress() {
        MeshtasticConfig cfg = validConfig();
        cfg.tcpAddress = null;
        assertThrows(IllegalArgumentException.class, cfg::validate,
                "validate() should throw for a null tcpAddress");
    }

    // --- scenario: config-port-zero ---

    @Test
    void validateRejectsPortZero() {
        MeshtasticConfig cfg = validConfig();
        cfg.tcpPort = 0;
        assertThrows(IllegalArgumentException.class, cfg::validate,
                "validate() should throw for port 0");
    }

    // --- scenario: config-port-overflow ---

    @Test
    void validateRejectsPortAbove65535() {
        MeshtasticConfig cfg = validConfig();
        cfg.tcpPort = 65536;
        assertThrows(IllegalArgumentException.class, cfg::validate,
                "validate() should throw for port > 65535");
    }

    // --- scenario: config-negative-delay ---

    @Test
    void validateRejectsNegativeReconnectDelay() {
        MeshtasticConfig cfg = validConfig();
        cfg.reconnectDelayMs = -1;
        assertThrows(IllegalArgumentException.class, cfg::validate,
                "validate() should throw for a negative reconnect delay");
    }

    // --- scenario: config-valid ---

    @Test
    void validatePassesForMinimalValidConfig() {
        MeshtasticConfig cfg = validConfig();
        assertDoesNotThrow(cfg::validate,
                "validate() should not throw for a well-formed config");
    }

    @Test
    void validatePassesForPort1() {
        MeshtasticConfig cfg = validConfig();
        cfg.tcpPort = 1;
        assertDoesNotThrow(cfg::validate, "Port 1 should be valid");
    }

    @Test
    void validatePassesForPort65535() {
        MeshtasticConfig cfg = validConfig();
        cfg.tcpPort = 65535;
        assertDoesNotThrow(cfg::validate, "Port 65535 should be valid");
    }

    @Test
    void validatePassesForZeroReconnectDelay() {
        MeshtasticConfig cfg = validConfig();
        cfg.reconnectDelayMs = 0;
        assertDoesNotThrow(cfg::validate, "Reconnect delay of 0 should be valid");
    }

    // --- helpers ---

    private MeshtasticConfig validConfig() {
        MeshtasticConfig cfg = new MeshtasticConfig();
        cfg.id = "test-sensor";
        cfg.tcpAddress = "192.168.1.100";
        cfg.tcpPort = 4403;
        cfg.reconnectDelayMs = 1000;
        return cfg;
    }
}