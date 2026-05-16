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

import org.sensorhub.api.config.DisplayInfo;
import org.sensorhub.api.sensor.SensorConfig;


/**
 * Configuration for the Meshtastic TCP sensor driver.
 *
 * <p>The driver connects to a running meshtastic-daemon process over TCP.
 * The daemon listens by default on port 4403.
 * See: https://meshtastic.org/docs/software/python/cli/#tcp</p>
 */
public class MeshtasticConfig extends SensorConfig {

    /** Default TCP port used by the meshtastic daemon. */
    public static final int DEFAULT_TCP_PORT = 4403;

    /** Default reconnect delay in milliseconds. */
    public static final int DEFAULT_RECONNECT_DELAY_MS = 5_000;

    /** Maximum length of a single protobuf frame accepted from the daemon. */
    public static final int MAX_FRAME_SIZE_BYTES = 512 * 1024;


    @DisplayInfo(label = "TCP Address",
            desc = "Hostname or IP address of the meshtastic daemon.")
    public String tcpAddress = "localhost";


    @DisplayInfo(label = "TCP Port",
            desc = "TCP port on which the meshtastic daemon is listening (default 4403).")
    public int tcpPort = DEFAULT_TCP_PORT;


    @DisplayInfo(label = "Reconnect Delay (ms)",
            desc = "Milliseconds to wait before attempting to reconnect after a connection failure.")
    public int reconnectDelayMs = DEFAULT_RECONNECT_DELAY_MS;


    @DisplayInfo(label = "Sensor Unique ID",
            desc = "Override for the sensor unique identifier (URN). "
                    + "Leave blank to auto-generate from the module ID.")
    public String sensorUID;


    /**
     * Validates that the configuration is internally consistent.
     *
     * @throws IllegalArgumentException if any field value is out of range.
     */
    public void validate() {
        if (tcpAddress == null || tcpAddress.isBlank()) {
            throw new IllegalArgumentException("tcpAddress must not be blank");
        }
        if (tcpPort <= 0 || tcpPort > 65535) {
            throw new IllegalArgumentException("tcpPort must be in range [1, 65535], got: " + tcpPort);
        }
        if (reconnectDelayMs < 0) {
            throw new IllegalArgumentException("reconnectDelayMs must be >= 0, got: " + reconnectDelayMs);
        }
    }
}