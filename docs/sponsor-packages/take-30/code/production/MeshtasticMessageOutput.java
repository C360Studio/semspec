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

import net.opengis.swe.v20.DataBlock;
import net.opengis.swe.v20.DataRecord;
import net.opengis.swe.v20.TextEncoding;
import org.sensorhub.api.data.DataEvent;
import org.sensorhub.impl.sensor.AbstractSensorOutput;
import org.vast.swe.SWEHelper;


/**
 * OSH streaming data output for Meshtastic mesh messages.
 *
 * <p>Each observation record contains:
 * <ul>
 *   <li>sampling time (ISO 8601 UTC)</li>
 *   <li>from_node — integer node ID of the sender</li>
 *   <li>to_node — integer node ID of the destination (0xFFFFFFFF = broadcast)</li>
 *   <li>channel — channel index (0–7)</li>
 *   <li>port_num — application port number identifying the payload type</li>
 *   <li>payload_text — UTF-8 decoded payload (empty string for non-text payloads)</li>
 *   <li>rx_snr — received signal-to-noise ratio in dB</li>
 *   <li>rx_rssi — received signal strength in dBm</li>
 * </ul>
 * </p>
 */
public class MeshtasticMessageOutput extends AbstractSensorOutput<MeshtasticSensor> {

    static final String OUTPUT_NAME = "meshMessages";
    static final String OUTPUT_LABEL = "Meshtastic Mesh Messages";

    private DataRecord outputStruct;
    private TextEncoding outputEncoding;


    MeshtasticMessageOutput(MeshtasticSensor parentSensor) {
        super(OUTPUT_NAME, parentSensor);
    }


    /**
     * Initialises the SWE data record structure and encoding.
     * Must be called once before the first call to {@link #publishObservation}.
     */
    void init() {
        SWEHelper swe = new SWEHelper();

        outputStruct = swe.createRecord()
                .name(OUTPUT_NAME)
                .label(OUTPUT_LABEL)
                .description("Messages received from the Meshtastic mesh network via TCP daemon.")
                .addSamplingTimeIsoUTC("time")
                .addField("from_node",
                        swe.createCount()
                                .label("From Node ID")
                                .description("Unique 32-bit node number of the sender.")
                                .build())
                .addField("to_node",
                        swe.createCount()
                                .label("To Node ID")
                                .description("Destination node number (0xFFFFFFFF = broadcast).")
                                .build())
                .addField("channel",
                        swe.createCount()
                                .label("Channel Index")
                                .description("Mesh channel this packet was sent on (0–7).")
                                .build())
                .addField("port_num",
                        swe.createCount()
                                .label("Port Number")
                                .description("Application-level port number identifying the payload type.")
                                .build())
                .addField("payload_text",
                        swe.createText()
                                .label("Payload Text")
                                .description("UTF-8 decoded payload for TEXT_MESSAGE_APP packets; "
                                        + "empty for other payload types.")
                                .build())
                .addField("rx_snr",
                        swe.createQuantity()
                                .label("RX SNR")
                                .description("Received signal-to-noise ratio reported by the receiver node.")
                                .uomCode("dB")
                                .build())
                .addField("rx_rssi",
                        swe.createQuantity()
                                .label("RX RSSI")
                                .description("Received signal strength indicator reported by the receiver node.")
                                .uomCode("dBm")
                                .build())
                .build();

        outputEncoding = swe.newTextEncoding(",", "\n");
    }


    /**
     * Publishes a received mesh packet as an OSH observation record.
     *
     * @param timeMs    wall-clock time in milliseconds since epoch
     * @param fromNode  sender node ID
     * @param toNode    destination node ID
     * @param channel   channel index
     * @param portNum   application port number
     * @param text      decoded text payload (may be empty)
     * @param rxSnr     received SNR in dB
     * @param rxRssi    received RSSI in dBm
     */
    void publishObservation(long timeMs, int fromNode, int toNode,
                            int channel, int portNum, String text,
                            float rxSnr, int rxRssi) {
        DataBlock dataBlock = (latestRecord == null)
                ? outputStruct.createDataBlock()
                : latestRecord.renew();

        double timeSec = timeMs / 1000.0;
        dataBlock.setDoubleValue(0, timeSec);
        dataBlock.setIntValue(1, fromNode);
        dataBlock.setIntValue(2, toNode);
        dataBlock.setIntValue(3, channel);
        dataBlock.setIntValue(4, portNum);
        dataBlock.setStringValue(5, text != null ? text : "");
        dataBlock.setDoubleValue(6, rxSnr);
        dataBlock.setDoubleValue(7, rxRssi);

        latestRecord = dataBlock;
        latestRecordTime = timeMs;
        eventHandler.publish(new DataEvent(timeMs, this, dataBlock));
    }


    @Override
    public DataRecord getRecordDescription() {
        return outputStruct;
    }


    @Override
    public TextEncoding getRecommendedEncoding() {
        return outputEncoding;
    }


    @Override
    public double getAverageSamplingPeriod() {
        // Meshtastic messages are event-driven; there is no fixed sampling period.
        return Double.NaN;
    }
}