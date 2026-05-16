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

import com.geeksville.mesh.MeshProtos.FromRadio;
import com.geeksville.mesh.MeshProtos.MeshPacket;
import com.geeksville.mesh.MeshProtos.PortNum;
import com.google.protobuf.InvalidProtocolBufferException;
import org.sensorhub.api.common.SensorHubException;
import org.sensorhub.impl.sensor.AbstractSensorModule;

import java.io.BufferedInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.atomic.AtomicBoolean;


/**
 * OSH sensor driver for the Meshtastic mesh networking platform.
 *
 * <p>The driver maintains a TCP connection to a running {@code meshtastic-daemon}
 * process and:
 * <ul>
 *   <li>publishes received {@link FromRadio} packets as OSH observations via
 *       {@link MeshtasticMessageOutput};</li>
 *   <li>exposes a control interface ({@link MeshtasticSendControl}) that lets
 *       the operator send text messages over the mesh;</li>
 *   <li>automatically reconnects when the TCP connection drops, updating the
 *       module status to CONNECTED / DISCONNECTED as appropriate.</li>
 * </ul>
 * </p>
 *
 * <h3>TCP frame format</h3>
 * <pre>
 *   [4 bytes] magic: 0x94 0xC3 0x00 0x00
 *   [4 bytes] payload length (big-endian uint32)
 *   [N bytes] serialised FromRadio protobuf
 * </pre>
 *
 * <p>The same layout is used for outgoing {@code ToRadio} frames.</p>
 */
public class MeshtasticSensor extends AbstractSensorModule<MeshtasticConfig> {

    /** URN prefix for auto-generated unique IDs. */
    static final String UID_PREFIX = "urn:osh:sensor:meshtastic:";

    /** XML ID prefix. */
    static final String XML_ID_PREFIX = "MESHTASTIC_";

    /** Meshtastic TCP stream magic header (big-endian uint32 = 0x94C3_0000). */
    static final int FRAME_MAGIC_INT = 0x94C30000;

    /** Number of bytes in the fixed frame header (magic + length). */
    static final int FRAME_HEADER_BYTES = 8;

    /** Maximum reconnect attempts before giving up entirely. Zero means unlimited. */
    static final int MAX_RECONNECT_ATTEMPTS = 0;

    // -------------------------------------------------------------------------
    // Driver state
    // -------------------------------------------------------------------------

    private MeshtasticMessageOutput messageOutput;
    private MeshtasticSendControl sendControl;

    /** The active TCP socket; null when not connected. */
    private volatile Socket tcpSocket;

    /** Output stream of the active TCP socket; null when not connected. */
    private volatile OutputStream tcpOut;

    /** Background thread that reads {@code FromRadio} frames from the daemon. */
    private Thread readerThread;

    /** Set to false to stop the reader thread. */
    private final AtomicBoolean running = new AtomicBoolean(false);

    /** Address string used for status messages. */
    private String daemonAddress;


    // -------------------------------------------------------------------------
    // AbstractModule lifecycle overrides
    // -------------------------------------------------------------------------

    @Override
    protected void doInit() throws SensorHubException {
        // Derive unique ID from config or generate from module ID.
        String uid = (config.sensorUID != null && !config.sensorUID.isBlank())
                ? config.sensorUID
                : UID_PREFIX + config.id;

        generateUniqueID(uid, null);
        generateXmlID(XML_ID_PREFIX, null);

        // Create and initialise the observation output.
        messageOutput = new MeshtasticMessageOutput(this);
        messageOutput.init();
        addOutput(messageOutput, false);

        // Create and initialise the send-text-message control interface.
        sendControl = new MeshtasticSendControl(this);
        sendControl.init();
        addControlInput(sendControl);

        daemonAddress = config.tcpAddress + ":" + config.tcpPort;
    }


    @Override
    protected void doStart() throws SensorHubException {
        config.validate();
        running.set(true);
        startReaderThread();
    }


    @Override
    protected void doStop() throws SensorHubException {
        running.set(false);
        closeSocket();
        if (readerThread != null) {
            readerThread.interrupt();
            try {
                readerThread.join(2_000);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
            }
            readerThread = null;
        }
        notifyConnectionStatus(false, daemonAddress);
    }


    // -------------------------------------------------------------------------
    // Public accessors used by MeshtasticSendControl
    // -------------------------------------------------------------------------

    /**
     * Returns the active output stream for sending {@code ToRadio} frames,
     * or {@code null} if the driver is not currently connected.
     */
    OutputStream getOutputStream() {
        return tcpOut;
    }


    // -------------------------------------------------------------------------
    // Connection and reader thread
    // -------------------------------------------------------------------------

    /**
     * Starts the background reader thread that manages the TCP connection and
     * processes incoming {@code FromRadio} frames.
     */
    private void startReaderThread() {
        readerThread = new Thread(this::readerLoop, "meshtastic-reader-" + config.id);
        readerThread.setDaemon(true);
        readerThread.start();
    }


    /**
     * The main loop of the reader thread.
     *
     * <p>Attempts to connect to the daemon, then reads frames until either
     * the driver is stopped or the connection drops.  On a connection failure
     * the loop waits {@link MeshtasticConfig#reconnectDelayMs} milliseconds and
     * retries.</p>
     */
    private void readerLoop() {
        int attempts = 0;
        final int maxAttempts = MAX_RECONNECT_ATTEMPTS;

        while (running.get()) {
            try {
                connectToDaemon();
                attempts = 0;
                notifyConnectionStatus(true, daemonAddress);
                readFramesUntilDisconnect();
            } catch (IOException e) {
                if (!running.get()) {
                    break; // normal stop
                }
                getLogger().warn("Connection to {} lost: {}", daemonAddress, e.getMessage());
            } finally {
                closeSocket();
                if (running.get()) {
                    notifyConnectionStatus(false, daemonAddress);
                }
            }

            if (!running.get()) {
                break;
            }

            attempts++;
            if (maxAttempts > 0 && attempts >= maxAttempts) {
                getLogger().error("Giving up after {} reconnect attempts to {}", attempts, daemonAddress);
                running.set(false);
                break;
            }

            sleepBeforeReconnect();
        }

        getLogger().info("Meshtastic reader thread exiting for {}", daemonAddress);
    }


    /**
     * Opens a TCP connection to the meshtastic daemon and stores the socket
     * and output stream.
     */
    private void connectToDaemon() throws IOException {
        getLogger().info("Connecting to meshtastic daemon at {}", daemonAddress);
        Socket socket = new Socket();
        socket.connect(new InetSocketAddress(config.tcpAddress, config.tcpPort), 5_000);
        socket.setSoTimeout(30_000); // 30-second read timeout
        tcpSocket = socket;
        tcpOut = socket.getOutputStream();
    }


    /**
     * Reads {@code FromRadio} frames from the daemon until the connection
     * closes or an I/O error occurs.
     */
    private void readFramesUntilDisconnect() throws IOException {
        InputStream in = new BufferedInputStream(tcpSocket.getInputStream());
        byte[] headerBuf = new byte[FRAME_HEADER_BYTES];

        while (running.get()) {
            readFully(in, headerBuf, FRAME_HEADER_BYTES);

            ByteBuffer hdr = ByteBuffer.wrap(headerBuf);
            int magic = hdr.getInt();
            int length = hdr.getInt();

            if (magic != FRAME_MAGIC_INT) {
                throw new IOException(String.format(
                        "Invalid frame magic: expected 0x%08X, got 0x%08X",
                        FRAME_MAGIC_INT, magic));
            }
            if (length < 0 || length > MeshtasticConfig.MAX_FRAME_SIZE_BYTES) {
                throw new IOException("Frame length out of bounds: " + length);
            }

            byte[] payload = new byte[length];
            readFully(in, payload, length);

            dispatchFrame(payload);
        }
    }


    /**
     * Deserialises a {@code FromRadio} protobuf frame and dispatches any
     * contained {@code MeshPacket} to the observation output.
     */
    private void dispatchFrame(byte[] payload) {
        FromRadio fromRadio;
        try {
            fromRadio = FromRadio.parseFrom(payload);
        } catch (InvalidProtocolBufferException e) {
            getLogger().warn("Received unparseable FromRadio frame ({}  bytes): {}", payload.length, e.getMessage());
            return;
        }

        if (fromRadio.hasPacket()) {
            handleMeshPacket(fromRadio.getPacket());
        }
    }


    /**
     * Extracts observation fields from a decoded {@link MeshPacket} and
     * publishes them to the message output.
     */
    private void handleMeshPacket(MeshPacket packet) {
        if (!packet.hasDecoded()) {
            return; // skip encrypted packets we cannot decode
        }

        long timeMs = System.currentTimeMillis();
        int fromNode = packet.getFrom();
        int toNode = packet.getTo();
        int channel = packet.getChannel();

        var data = packet.getDecoded();
        int portNum = data.getPortnum().getNumber();
        String text = "";
        if (data.getPortnum() == PortNum.TEXT_MESSAGE_APP) {
            text = data.getPayload().toString(StandardCharsets.UTF_8);
        }

        float rxSnr = packet.getRxSnr();
        int rxRssi = packet.getRxRssi();

        messageOutput.publishObservation(timeMs, fromNode, toNode, channel,
                portNum, text, rxSnr, rxRssi);
    }


    // -------------------------------------------------------------------------
    // Socket / stream helpers
    // -------------------------------------------------------------------------

    /**
     * Closes the TCP socket and clears the socket and output-stream references.
     */
    private void closeSocket() {
        tcpOut = null;
        Socket sock = tcpSocket;
        tcpSocket = null;
        if (sock != null && !sock.isClosed()) {
            try {
                sock.close();
            } catch (IOException e) {
                getLogger().debug("Error closing socket: {}", e.getMessage());
            }
        }
    }


    /**
     * Reads exactly {@code len} bytes from {@code in} into {@code buf},
     * blocking until all bytes are available.
     *
     * @throws IOException if the stream ends before {@code len} bytes are read
     *                     or if an I/O error occurs
     */
    private void readFully(InputStream in, byte[] buf, int len) throws IOException {
        int read = 0;
        while (read < len) {
            int n = in.read(buf, read, len - read);
            if (n == -1) {
                throw new IOException("Stream closed after reading " + read + " of " + len + " bytes");
            }
            read += n;
        }
    }


    /**
     * Sleeps for the configured reconnect delay, returning early if the thread
     * is interrupted.
     */
    private void sleepBeforeReconnect() {
        try {
            Thread.sleep(config.reconnectDelayMs);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }
}