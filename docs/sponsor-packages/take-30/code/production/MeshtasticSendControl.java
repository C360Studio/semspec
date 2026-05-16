/***************************** BEGIN LICENSE BLOCK ***************************
 The contents of this file are subject to the Mozilla Public License, v. 2.0.
 If a copy of the MPL was not distributed with this file, You can obtain one
 at http://mozilla.org/MPL/2.0/.

 Software distributed under the License is distributed on an "AS IS" basis,
 WITHOUT WARRANTY OF ANY KIND, either express or implied. See the License
 for the specific language governing rights and limitations under the License.

 Copyright (C) 2024 Sensia Software LLC. All Rights Reserved.
 ******************************* END LICENSE BLOCK ***************************/

package org.sensorhub.impl.sensor.meshtastic;

import com.geeksville.mesh.MeshProtos.Data;
import com.geeksville.mesh.MeshProtos.MeshPacket;
import com.geeksville.mesh.MeshProtos.PortNum;
import com.geeksville.mesh.MeshProtos.ToRadio;
import com.google.protobuf.ByteString;
import net.opengis.swe.v20.DataBlock;
import net.opengis.swe.v20.DataComponent;
import org.sensorhub.api.command.CommandException;
import org.sensorhub.impl.sensor.AbstractSensorControl;
import org.vast.swe.SWEHelper;

import java.io.IOException;
import java.io.OutputStream;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;


/**
 * OSH command interface for sending text messages through the Meshtastic mesh.
 *
 * <p>The command record contains:
 * <ul>
 *   <li>to_node   — destination node ID (0 means broadcast)</li>
 *   <li>channel   — channel index (0–7)</li>
 *   <li>text      — the text message to send</li>
 * </ul>
 * </p>
 */
public class MeshtasticSendControl extends AbstractSensorControl<MeshtasticSensor> {

    static final String CONTROL_NAME = "sendTextMessage";

    /** Meshtastic TCP stream magic header bytes (big-endian uint32 = 0x94C3_0000). */
    static final byte[] FRAME_MAGIC = {(byte) 0x94, (byte) 0xC3, 0x00, 0x00};

    /** Maximum allowed text length for a single outgoing message. */
    static final int MAX_TEXT_BYTES = 237;

    private DataComponent commandStruct;


    MeshtasticSendControl(MeshtasticSensor parentSensor) {
        super(CONTROL_NAME, parentSensor);
    }


    /**
     * Initialises the SWE command record structure.
     */
    void init() {
        SWEHelper swe = new SWEHelper();

        commandStruct = swe.createRecord()
                .name(CONTROL_NAME)
                .label("Send Text Message")
                .description("Sends a UTF-8 text message over the Meshtastic mesh network.")
                .addField("to_node",
                        swe.createCount()
                                .label("Destination Node ID")
                                .description("Target node number; 0 or 0xFFFFFFFF for broadcast.")
                                .build())
                .addField("channel",
                        swe.createCount()
                                .label("Channel Index")
                                .description("Channel to use for this message (0–7).")
                                .build())
                .addField("text",
                        swe.createText()
                                .label("Message Text")
                                .description("UTF-8 encoded text content of the message "
                                        + "(max " + MAX_TEXT_BYTES + " bytes).")
                                .build())
                .build();
    }


    @Override
    public DataComponent getCommandDescription() {
        return commandStruct;
    }


    /**
     * Executes the send-text-message command.
     *
     * <p>Validates the command fields and writes a framed {@code ToRadio} protobuf
     * message to the active TCP output stream.</p>
     *
     * @param cmdData command data block (to_node, channel, text)
     * @return {@code true} if the message was queued for transmission
     * @throws CommandException if the command is invalid or the connection is unavailable
     */
    @Override
    protected boolean execCommand(DataBlock cmdData) throws CommandException {
        int toNode = cmdData.getIntValue(0);
        int channel = cmdData.getIntValue(1);
        String text = cmdData.getStringValue(2);

        validateCommand(toNode, channel, text);

        OutputStream out = parentSensor.getOutputStream();
        if (out == null) {
            throw new CommandException("Cannot send — not connected to meshtastic daemon");
        }

        try {
            writeTextMessage(out, toNode, channel, text);
            return true;
        } catch (IOException e) {
            throw new CommandException("Failed to transmit message: " + e.getMessage(), e);
        }
    }


    // -------------------------------------------------------------------------
    // Private helpers
    // -------------------------------------------------------------------------

    private void validateCommand(int toNode, int channel, String text) throws CommandException {
        if (channel < 0 || channel > 7) {
            throw new CommandException("channel must be in [0, 7], got: " + channel);
        }
        if (text == null || text.isEmpty()) {
            throw new CommandException("text must not be empty");
        }
        byte[] textBytes = text.getBytes(StandardCharsets.UTF_8);
        if (textBytes.length > MAX_TEXT_BYTES) {
            throw new CommandException("text exceeds maximum of " + MAX_TEXT_BYTES
                    + " bytes (encoded length: " + textBytes.length + ")");
        }
    }


    /**
     * Writes a framed {@code ToRadio} message containing a text {@code MeshPacket}
     * to {@code out}.
     *
     * <p>Frame layout:
     * <pre>
     *   [4 bytes] magic header: 0x94 0xC3 0x00 0x00
     *   [4 bytes] payload length (big-endian uint32)
     *   [N bytes] serialised ToRadio protobuf
     * </pre>
     * </p>
     */
    private void writeTextMessage(OutputStream out, int toNode, int channel, String text)
            throws IOException {
        int destinationNode = (toNode == 0) ? 0xFFFFFFFF : toNode;

        Data data = Data.newBuilder()
                .setPortnum(PortNum.TEXT_MESSAGE_APP)
                .setPayload(ByteString.copyFromUtf8(text))
                .build();

        MeshPacket packet = MeshPacket.newBuilder()
                .setTo(destinationNode)
                .setChannel(channel)
                .setDecoded(data)
                .build();

        ToRadio toRadio = ToRadio.newBuilder()
                .setPacket(packet)
                .build();

        byte[] payload = toRadio.toByteArray();

        ByteBuffer header = ByteBuffer.allocate(8);
        header.put(FRAME_MAGIC);
        header.putInt(payload.length);

        synchronized (out) {
            out.write(header.array());
            out.write(payload);
            out.flush();
        }
    }
}