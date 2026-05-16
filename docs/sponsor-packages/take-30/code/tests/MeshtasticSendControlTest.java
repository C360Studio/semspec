/*
 * Tests for MeshtasticSendControl.
 *
 * Acceptance criteria:
 *   - Command description has 3 fields (scenario: control-structure)
 *   - execCommand fails with CommandException when not connected (scenario: control-not-connected)
 *   - execCommand fails for empty text (scenario: control-empty-text)
 *   - execCommand fails for channel out of range (scenario: control-invalid-channel)
 *   - execCommand fails for text exceeding max bytes (scenario: control-text-too-long)
 *   - execCommand writes framed ToRadio to output stream (scenario: control-writes-frame)
 */

package org.sensorhub.impl.sensor.meshtastic;

import com.geeksville.mesh.MeshProtos.ToRadio;
import net.opengis.swe.v20.DataBlock;
import net.opengis.swe.v20.DataComponent;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.mockito.Mockito;
import org.sensorhub.api.command.CommandException;
import org.vast.data.DataBlockMixed;

import java.io.ByteArrayOutputStream;
import java.nio.ByteBuffer;

import static org.junit.jupiter.api.Assertions.*;
import static org.mockito.Mockito.when;

/**
 * Unit tests for {@link MeshtasticSendControl}.
 */
class MeshtasticSendControlTest {

    private MeshtasticSensor mockSensor;
    private MeshtasticSendControl control;

    @BeforeEach
    void setUp() {
        mockSensor = Mockito.mock(MeshtasticSensor.class);
        control = new MeshtasticSendControl(mockSensor);
        control.init();
    }

    // --- scenario: control-structure ---

    @Test
    void initProducesCommandDescription() {
        DataComponent desc = control.getCommandDescription();
        assertNotNull(desc, "getCommandDescription() must not be null after init()");
    }

    @Test
    void commandDescriptionHasThreeFields() {
        DataComponent desc = control.getCommandDescription();
        // to_node, channel, text
        assertEquals(3, desc.getComponentCount(),
                "Command record must have 3 fields: to_node, channel, text");
    }

    // --- scenario: control-not-connected ---

    @Test
    void execCommandThrowsWhenNotConnected() {
        when(mockSensor.getOutputStream()).thenReturn(null);
        DataBlock cmd = buildCommand(0, 0, "hello");

        assertThrows(CommandException.class, () -> control.execCommand(cmd),
                "execCommand must throw CommandException when output stream is null");
    }

    // --- scenario: control-empty-text ---

    @Test
    void execCommandThrowsForEmptyText() {
        when(mockSensor.getOutputStream()).thenReturn(new ByteArrayOutputStream());
        DataBlock cmd = buildCommand(0, 0, "");

        assertThrows(CommandException.class, () -> control.execCommand(cmd),
                "execCommand must throw CommandException for empty text");
    }

    @Test
    void execCommandThrowsForNullText() {
        when(mockSensor.getOutputStream()).thenReturn(new ByteArrayOutputStream());
        DataBlock cmd = buildCommand(0, 0, null);

        assertThrows(CommandException.class, () -> control.execCommand(cmd),
                "execCommand must throw CommandException for null text");
    }

    // --- scenario: control-invalid-channel ---

    @Test
    void execCommandThrowsForChannelBelowZero() {
        when(mockSensor.getOutputStream()).thenReturn(new ByteArrayOutputStream());
        DataBlock cmd = buildCommand(0, -1, "test");

        assertThrows(CommandException.class, () -> control.execCommand(cmd),
                "execCommand must throw for channel < 0");
    }

    @Test
    void execCommandThrowsForChannelAboveSeven() {
        when(mockSensor.getOutputStream()).thenReturn(new ByteArrayOutputStream());
        DataBlock cmd = buildCommand(0, 8, "test");

        assertThrows(CommandException.class, () -> control.execCommand(cmd),
                "execCommand must throw for channel > 7");
    }

    // --- scenario: control-text-too-long ---

    @Test
    void execCommandThrowsForTextExceedingMaxBytes() {
        when(mockSensor.getOutputStream()).thenReturn(new ByteArrayOutputStream());
        String longText = "x".repeat(MeshtasticSendControl.MAX_TEXT_BYTES + 1);
        DataBlock cmd = buildCommand(0, 0, longText);

        assertThrows(CommandException.class, () -> control.execCommand(cmd),
                "execCommand must throw for text exceeding MAX_TEXT_BYTES");
    }

    @Test
    void execCommandAcceptsTextAtExactMaxBytes() throws CommandException {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        when(mockSensor.getOutputStream()).thenReturn(out);

        String maxText = "x".repeat(MeshtasticSendControl.MAX_TEXT_BYTES);
        DataBlock cmd = buildCommand(0, 0, maxText);

        assertTrue(control.execCommand(cmd),
                "execCommand should return true for text exactly at max bytes");
    }

    // --- scenario: control-writes-frame ---

    @Test
    void execCommandWritesFrameWithCorrectMagic() throws Exception {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        when(mockSensor.getOutputStream()).thenReturn(out);

        control.execCommand(buildCommand(0, 0, "hi"));

        byte[] written = out.toByteArray();
        assertTrue(written.length >= 8, "Frame must be at least 8 bytes");

        // Check magic header
        assertArrayEquals(MeshtasticSendControl.FRAME_MAGIC,
                new byte[]{written[0], written[1], written[2], written[3]},
                "First 4 bytes must be the frame magic");
    }

    @Test
    void execCommandWritesValidProtobufPayload() throws Exception {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        when(mockSensor.getOutputStream()).thenReturn(out);

        control.execCommand(buildCommand(42, 1, "Hello"));

        byte[] written = out.toByteArray();
        int payloadLength = ByteBuffer.wrap(written, 4, 4).getInt();
        byte[] payload = new byte[payloadLength];
        System.arraycopy(written, 8, payload, 0, payloadLength);

        // Should be parseable as a ToRadio protobuf
        ToRadio toRadio = ToRadio.parseFrom(payload);
        assertTrue(toRadio.hasPacket(), "Payload must contain a MeshPacket");
        assertEquals(1, toRadio.getPacket().getChannel(), "Channel should be 1");
    }

    @Test
    void execCommandSetsToNodeToBroadcastWhenZero() throws Exception {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        when(mockSensor.getOutputStream()).thenReturn(out);

        control.execCommand(buildCommand(0, 0, "broadcast"));

        byte[] written = out.toByteArray();
        int payloadLength = ByteBuffer.wrap(written, 4, 4).getInt();
        byte[] payload = new byte[payloadLength];
        System.arraycopy(written, 8, payload, 0, payloadLength);

        ToRadio toRadio = ToRadio.parseFrom(payload);
        assertEquals(0xFFFFFFFFL, Integer.toUnsignedLong(toRadio.getPacket().getTo()),
                "to_node 0 should be expanded to 0xFFFFFFFF (broadcast)");
    }

    // --- helpers ---

    /**
     * Creates a synthetic DataBlock for the send-text-message command.
     * Field order: [0]=to_node (int), [1]=channel (int), [2]=text (String).
     */
    private DataBlock buildCommand(int toNode, int channel, String text) {
        // We need to build a DataBlock that matches the command structure.
        // Use the record description to create one.
        DataBlock block = control.getCommandDescription().createDataBlock();
        block.setIntValue(0, toNode);
        block.setIntValue(1, channel);
        block.setStringValue(2, text);
        return block;
    }
}