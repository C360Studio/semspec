/*
 * Integration tests for MeshtasticSensor.
 *
 * These tests spin up a real ServerSocket to act as the meshtastic daemon
 * and verify end-to-end behaviour across the TCP connection.
 *
 * Acceptance criteria:
 *   - Driver connects to mock daemon and fires CONNECTED event (scenario: driver-connects)
 *   - Driver decodes FromRadio frame and publishes DataEvent (scenario: driver-receives-packet)
 *   - Driver handles daemon disconnect and fires DISCONNECTED event (scenario: driver-disconnects)
 *   - Driver sends ToRadio frame when sendTextMessage command is submitted
 *     (scenario: meshtastic-daemon-bidirectional)
 *   - E2E: Node transmits → Daemon receives → Driver reads → OSH event published
 *     (scenario: e2e-node-transmit-to-osh-event)
 */

package org.sensorhub.impl.sensor.meshtastic;

import com.geeksville.mesh.MeshProtos.Data;
import com.geeksville.mesh.MeshProtos.FromRadio;
import com.geeksville.mesh.MeshProtos.MeshPacket;
import com.geeksville.mesh.MeshProtos.PortNum;
import com.geeksville.mesh.MeshProtos.ToRadio;
import com.google.protobuf.ByteString;
import net.opengis.swe.v20.DataBlock;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.sensorhub.api.data.DataEvent;
import org.sensorhub.api.event.IEventListener;
import org.sensorhub.api.module.ModuleEvent;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.ServerSocket;
import java.net.Socket;
import java.nio.ByteBuffer;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import static org.junit.jupiter.api.Assertions.*;

/**
 * Integration tests for {@link MeshtasticSensor} using a mock TCP server.
 *
 * <p>Each test starts a {@link ServerSocket} on an ephemeral port, configures
 * the driver to connect to {@code localhost:<port>}, and then drives the
 * server side to exercise the protocol.</p>
 */
@Timeout(30) // guard against hangs
class MeshtasticSensorIntegrationTest {

    /** Magic header written at the start of every TCP frame (big-endian uint32). */
    private static final byte[] FRAME_MAGIC = {(byte) 0x94, (byte) 0xC3, 0x00, 0x00};

    private ServerSocket serverSocket;
    private MeshtasticSensor sensor;
    private MeshtasticConfig config;
    private int port;

    @BeforeEach
    void setUp() throws IOException {
        // Bind to any free port so tests can run in parallel.
        serverSocket = new ServerSocket(0);
        port = serverSocket.getLocalPort();

        config = new MeshtasticConfig();
        config.id = "integ-test";
        config.tcpAddress = "localhost";
        config.tcpPort = port;
        config.reconnectDelayMs = 200; // short delay so tests complete quickly
        config.sensorUID = "urn:osh:sensor:meshtastic:integ-test";

        sensor = new MeshtasticSensor();
        sensor.setConfiguration(config);
    }

    @AfterEach
    void tearDown() throws Exception {
        if (sensor != null) {
            sensor.stop();
        }
        if (serverSocket != null && !serverSocket.isClosed()) {
            serverSocket.close();
        }
    }

    // -------------------------------------------------------------------------
    // scenario: driver-connects
    // -------------------------------------------------------------------------

    @Test
    void driverConnectsToMockDaemonAndIsNotNull() throws Exception {
        CountDownLatch clientConnected = new CountDownLatch(1);
        Thread serverThread = new Thread(() -> {
            try {
                Socket client = serverSocket.accept();
                clientConnected.countDown();
                // Keep connection open long enough for the latch to be checked.
                Thread.sleep(500);
                client.close();
            } catch (Exception ignored) {}
        });
        serverThread.setDaemon(true);
        serverThread.start();

        sensor.init();
        sensor.start();

        assertTrue(clientConnected.await(5, TimeUnit.SECONDS),
                "Driver should connect to the mock daemon within 5 s");
    }

    // -------------------------------------------------------------------------
    // scenario: driver-receives-packet  (also covers e2e-node-transmit-to-osh-event)
    // -------------------------------------------------------------------------

    /**
     * E2E scenario: mesh node transmits packet → daemon receives → driver reads
     * → OSH event bus contains the parsed meshtastic message.
     */
    @Test
    void driverReceivesFromRadioFrameAndPublishesDataEvent() throws Exception {
        final String expectedText = "Hello OSH from Meshtastic!";
        final int senderNode = 0xDEAD;

        // Latch that trips when the driver publishes a DataEvent.
        CountDownLatch eventLatch = new CountDownLatch(1);
        AtomicReference<DataBlock> capturedBlock = new AtomicReference<>();

        IEventListener listener = event -> {
            if (event instanceof DataEvent de) {
                capturedBlock.set(de.getRecords()[0]);
                eventLatch.countDown();
            }
        };

        // Register listener before start so we don't miss the event.
        sensor.init();
        sensor.getOutputs().get(MeshtasticMessageOutput.OUTPUT_NAME)
                .registerListener(listener);

        // Start server — accept connection, send one FromRadio frame, then close.
        Thread serverThread = new Thread(() -> {
            try (Socket client = serverSocket.accept()) {
                sendTextMessageFrame(client.getOutputStream(), senderNode, expectedText);
                // Give the driver time to process before closing.
                Thread.sleep(200);
            } catch (Exception ignored) {}
        });
        serverThread.setDaemon(true);
        serverThread.start();

        sensor.start();

        assertTrue(eventLatch.await(10, TimeUnit.SECONDS),
                "Driver must publish a DataEvent within 10 s");

        DataBlock block = capturedBlock.get();
        assertNotNull(block, "Captured DataBlock must not be null");

        // Field index 1 = from_node, index 5 = payload_text
        assertEquals(senderNode, block.getIntValue(1),
                "from_node must match the sender node ID");
        assertEquals(expectedText, block.getStringValue(5),
                "payload_text must match the sent text");
    }

    // -------------------------------------------------------------------------
    // scenario: driver-disconnects
    // -------------------------------------------------------------------------

    @Test
    void driverFiresDisconnectedEventWhenDaemonClosesConnection() throws Exception {
        CountDownLatch disconnectedLatch = new CountDownLatch(1);

        sensor.init();

        // Listen on the sensor's event handler for a DISCONNECTED module event.
        sensor.registerListener(event -> {
            if (event instanceof ModuleEvent me
                    && me.getType() == ModuleEvent.Type.DISCONNECTED) {
                disconnectedLatch.countDown();
            }
        });

        // Server: accept, then immediately close.
        Thread serverThread = new Thread(() -> {
            try (Socket client = serverSocket.accept()) {
                // Close after a short delay to let the driver reach CONNECTED state.
                Thread.sleep(100);
            } catch (Exception ignored) {}
        });
        serverThread.setDaemon(true);
        serverThread.start();

        sensor.start();

        assertTrue(disconnectedLatch.await(10, TimeUnit.SECONDS),
                "Driver must fire a DISCONNECTED event when the daemon closes the socket");
    }

    // -------------------------------------------------------------------------
    // scenario: meshtastic-daemon-bidirectional
    // -------------------------------------------------------------------------

    /**
     * Verifies that the driver can connect to the meshtastic daemon over TCP,
     * send a message, and handle incoming messages.
     */
    @Test
    void bidirectionalCommunicationWorksCorrectly() throws Exception {
        final String messageText = "Bidirectional test";
        final int senderNode = 0xBEEF;

        // Track incoming observation from driver
        CountDownLatch receiveEventLatch = new CountDownLatch(1);
        AtomicReference<DataBlock> receivedBlock = new AtomicReference<>();

        // Track outgoing message received by mock server
        BlockingQueue<byte[]> serverReceivedFrames = new ArrayBlockingQueue<>(10);

        sensor.init();
        sensor.getOutputs().get(MeshtasticMessageOutput.OUTPUT_NAME)
                .registerListener(event -> {
                    if (event instanceof DataEvent de) {
                        receivedBlock.set(de.getRecords()[0]);
                        receiveEventLatch.countDown();
                    }
                });

        // Server: accept, send a FromRadio frame, then read a ToRadio frame.
        Thread serverThread = new Thread(() -> {
            try (Socket client = serverSocket.accept()) {
                InputStream in = client.getInputStream();
                OutputStream out = client.getOutputStream();

                // Step 1: send a message to the driver.
                sendTextMessageFrame(out, senderNode, messageText);

                // Step 2: read the ToRadio frame the driver sends back
                //         (triggered by the control test below).
                byte[] header = new byte[8];
                int read = 0;
                while (read < 8) {
                    int n = in.read(header, read, 8 - read);
                    if (n == -1) break;
                    read += n;
                }
                if (read == 8) {
                    int length = ByteBuffer.wrap(header, 4, 4).getInt();
                    byte[] payload = new byte[length];
                    int payloadRead = 0;
                    while (payloadRead < length) {
                        int n = in.read(payload, payloadRead, length - payloadRead);
                        if (n == -1) break;
                        payloadRead += n;
                    }
                    serverReceivedFrames.offer(payload);
                }

                Thread.sleep(300);
            } catch (Exception ignored) {}
        });
        serverThread.setDaemon(true);
        serverThread.start();

        sensor.start();

        // Wait for the incoming observation.
        assertTrue(receiveEventLatch.await(10, TimeUnit.SECONDS),
                "Driver must receive and publish the FromRadio packet");

        DataBlock block = receivedBlock.get();
        assertEquals(senderNode, block.getIntValue(1), "from_node must match");
        assertEquals(messageText, block.getStringValue(5), "payload_text must match");

        // Now send a message from the driver → daemon.
        waitForOutputStream(sensor, 5_000);
        MeshtasticSendControl control = (MeshtasticSendControl)
                sensor.getCommandInputs().get(MeshtasticSendControl.CONTROL_NAME);
        assertNotNull(control, "sendTextMessage control must be registered");

        DataBlock cmd = control.getCommandDescription().createDataBlock();
        cmd.setIntValue(0, 0);      // broadcast
        cmd.setIntValue(1, 0);      // channel 0
        cmd.setStringValue(2, "Reply from OSH");
        control.execCommand(cmd);

        // Verify the server received a valid ToRadio frame.
        byte[] payload = serverReceivedFrames.poll(5, TimeUnit.SECONDS);
        assertNotNull(payload, "Mock daemon must receive a ToRadio frame");
        ToRadio toRadio = ToRadio.parseFrom(payload);
        assertTrue(toRadio.hasPacket(), "ToRadio must contain a MeshPacket");
        assertEquals("Reply from OSH",
                toRadio.getPacket().getDecoded().getPayload().toStringUtf8(),
                "Payload text must match the submitted command");
    }

    // -------------------------------------------------------------------------
    // Helpers
    // -------------------------------------------------------------------------

    /**
     * Writes a framed {@code FromRadio} message containing a text packet from
     * {@code senderNode} with body {@code text} to {@code out}.
     */
    private static void sendTextMessageFrame(OutputStream out, int senderNode, String text)
            throws IOException {
        Data data = Data.newBuilder()
                .setPortnum(PortNum.TEXT_MESSAGE_APP)
                .setPayload(ByteString.copyFromUtf8(text))
                .build();

        MeshPacket packet = MeshPacket.newBuilder()
                .setFrom(senderNode)
                .setTo(0xFFFFFFFF)
                .setChannel(0)
                .setDecoded(data)
                .setRxSnr(6.5f)
                .setRxRssi(-90)
                .build();

        FromRadio fromRadio = FromRadio.newBuilder()
                .setNum(1)
                .setPacket(packet)
                .build();

        byte[] payload = fromRadio.toByteArray();
        ByteBuffer header = ByteBuffer.allocate(8);
        header.put(FRAME_MAGIC);
        header.putInt(payload.length);

        out.write(header.array());
        out.write(payload);
        out.flush();
    }

    /**
     * Waits until the sensor's output stream becomes available (non-null),
     * polling every 50 ms up to {@code timeoutMs} milliseconds.
     */
    private static void waitForOutputStream(MeshtasticSensor sensor, long timeoutMs)
            throws InterruptedException {
        long deadline = System.currentTimeMillis() + timeoutMs;
        while (sensor.getOutputStream() == null && System.currentTimeMillis() < deadline) {
            Thread.sleep(50);
        }
    }
}