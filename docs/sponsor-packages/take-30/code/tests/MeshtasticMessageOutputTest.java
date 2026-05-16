/*
 * Tests for MeshtasticMessageOutput.
 *
 * Acceptance criteria:
 *   - init() produces a DataRecord with 8 fields (scenario: output-record-structure)
 *   - publishObservation() fires a DataEvent (scenario: output-publishes-event)
 *   - publishObservation() updates latestRecord (scenario: output-latest-record)
 *   - publishObservation() encodes all fields correctly (scenario: output-field-values)
 *   - null text is converted to empty string (scenario: output-null-text)
 */

package org.sensorhub.impl.sensor.meshtastic;

import net.opengis.swe.v20.DataRecord;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.mockito.Mockito;
import org.sensorhub.api.data.DataEvent;
import org.sensorhub.api.event.IEventListener;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.*;

/**
 * Unit tests for {@link MeshtasticMessageOutput}.
 */
class MeshtasticMessageOutputTest {

    private MeshtasticSensor mockSensor;
    private MeshtasticMessageOutput output;

    @BeforeEach
    void setUp() {
        mockSensor = Mockito.mock(MeshtasticSensor.class);
        // getUniqueIdentifier() is required by DataEvent when it tries to resolve FOI list
        when(mockSensor.getUniqueIdentifier()).thenReturn("urn:osh:sensor:meshtastic:test");
        when(mockSensor.getCurrentFeaturesOfInterest()).thenReturn(java.util.Collections.emptyMap());
        output = new MeshtasticMessageOutput(mockSensor);
        output.init();
    }

    // --- scenario: output-record-structure ---

    @Test
    void initProducesDataRecordWithCorrectName() {
        DataRecord rec = output.getRecordDescription();
        assertNotNull(rec, "getRecordDescription() must not be null after init()");
        assertEquals(MeshtasticMessageOutput.OUTPUT_NAME, rec.getName(),
                "Record name must match OUTPUT_NAME");
    }

    @Test
    void initProducesDataRecordWithEightComponents() {
        DataRecord rec = output.getRecordDescription();
        // time, from_node, to_node, channel, port_num, payload_text, rx_snr, rx_rssi
        assertEquals(8, rec.getComponentCount(),
                "DataRecord must have 8 components");
    }

    @Test
    void outputHasRecommendedEncoding() {
        assertNotNull(output.getRecommendedEncoding(),
                "getRecommendedEncoding() must not be null");
    }

    @Test
    void outputHasNaNSamplingPeriod() {
        assertTrue(Double.isNaN(output.getAverageSamplingPeriod()),
                "getAverageSamplingPeriod() must be NaN for event-driven output");
    }

    // --- scenario: output-publishes-event ---

    @Test
    void publishObservationFiresDataEvent() {
        IEventListener listener = Mockito.mock(IEventListener.class);
        output.registerListener(listener);

        output.publishObservation(System.currentTimeMillis(),
                0x1234, 0xFFFFFFFF, 0, 1, "hello", 7.5f, -85);

        verify(listener, times(1)).handleEvent(any(DataEvent.class));
    }

    // --- scenario: output-latest-record ---

    @Test
    void publishObservationUpdatesLatestRecord() {
        assertNull(output.getLatestRecord(), "latestRecord must be null before first publish");

        long timeMs = System.currentTimeMillis();
        output.publishObservation(timeMs, 1, 2, 3, 4, "test", 5.0f, -90);

        assertNotNull(output.getLatestRecord(), "latestRecord must be non-null after publish");
        assertEquals(timeMs, output.getLatestRecordTime(),
                "latestRecordTime must match the publish timeMs");
    }

    // --- scenario: output-field-values ---

    @Test
    void publishObservationEncodesFromNodeCorrectly() {
        long timeMs = System.currentTimeMillis();
        output.publishObservation(timeMs, 0xABCD, 0, 1, 1, "msg", 0f, 0);

        assertEquals(0xABCD, output.getLatestRecord().getIntValue(1),
                "from_node must be stored at index 1");
    }

    @Test
    void publishObservationEncodesToNodeCorrectly() {
        long timeMs = System.currentTimeMillis();
        output.publishObservation(timeMs, 0, 0xFFFFFFFF, 0, 0, "", 0f, 0);

        assertEquals(0xFFFFFFFF, output.getLatestRecord().getIntValue(2),
                "to_node (broadcast) must be stored at index 2");
    }

    @Test
    void publishObservationEncodesPayloadTextCorrectly() {
        long timeMs = System.currentTimeMillis();
        String text = "Hello Meshtastic!";
        output.publishObservation(timeMs, 1, 2, 0, 1, text, 0f, 0);

        assertEquals(text, output.getLatestRecord().getStringValue(5),
                "payload_text must be stored at index 5");
    }

    @Test
    void publishObservationEncodesRxSnrCorrectly() {
        long timeMs = System.currentTimeMillis();
        output.publishObservation(timeMs, 1, 2, 0, 0, "", 8.75f, -80);

        assertEquals(8.75, output.getLatestRecord().getDoubleValue(6), 0.001,
                "rx_snr must be stored at index 6");
    }

    @Test
    void publishObservationEncodesRxRssiCorrectly() {
        long timeMs = System.currentTimeMillis();
        output.publishObservation(timeMs, 1, 2, 0, 0, "", 0f, -110);

        assertEquals(-110, output.getLatestRecord().getDoubleValue(7), 0.001,
                "rx_rssi must be stored at index 7");
    }

    // --- scenario: output-null-text ---

    @Test
    void publishObservationConvertsNullTextToEmptyString() {
        long timeMs = System.currentTimeMillis();
        output.publishObservation(timeMs, 1, 2, 0, 0, null, 0f, 0);

        String stored = output.getLatestRecord().getStringValue(5);
        assertEquals("", stored, "null text must be stored as empty string");
    }
}