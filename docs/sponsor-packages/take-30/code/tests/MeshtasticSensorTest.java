/*
 * Unit tests for MeshtasticSensor.
 *
 * These tests mock the TCP layer using an in-process ServerSocket bound to
 * an ephemeral port, allowing deterministic verification of sensor-level
 * behaviour without a real meshtastic-daemon process.
 *
 * Acceptance criteria covered:
 *   - Successful connection status updates (scenario: sensor-connected-status)
 *   - Payload receiving and publishing as OSH observations
 *     (scenario: sensor-receives-payload)
 *   - Outgoing message transmission via the send control
 *     (scenario: sensor-outgoing-message)
 *   - Failure handling for unreachable addresses
 *     (scenario: sensor-failure-unreachable)
 *   - init() registers required outputs and controls (scenario: sensor-init)
 *   - stop() transitions driver to disconnected (scenario: sensor-stop)
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
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

import static org.junit.jupiter.api.Assertions.*;

/**
 * Unit tests for {@link MeshtasticSensor} using a mock TCP server.
 *
 * <p>Each test that needs a live connection creates a {@link ServerSocket} on
 * an ephemeral port and configures the sensor to target {@code localhost:<port>}.
 * Tests that verify failure handling deliberately omit the server so that the
 * driver encounters a connection-refused error.</p>
 */
@Timeout(30) // guard against any test hanging indefinitely
class MeshtasticSensorTest {

    /** Frame magic written by the daemon before each protobuf payload. */
    private static final byte[] FRAME_MAGIC = {(byte) 0x94, (byte) 0xC3, 0x00, 0x00};

    // Populated by setUp(); tests that need no server close it immediately.
    private ServerSocket serverSocket;
    private int serverPort;

    private MeshtasticSensor sensor;
    private MeshtasticConfig config;

    @BeforeEach
    void setUp() throws IOException {
        // Bind to any free port; tests that don't need a server close it themselves.
        serverSocket = new ServerSocket(0);
        serverPort = serverSocket.getLocalPort();

        config = new MeshtasticConfig();
        config.id = "unit-test-sensor";
        config.tcpAddress = "localhost";
        config.tcpPort = serverPort;
        config.reconnectDelayMs = 50; // very short so failure tests finish quickly
        config.sensorUID = "urn:osh:sensor:meshtastic:unit-test";

        sensor = new MeshtasticSensor();
        sensor.setConfiguration(config);
    }

    @AfterEach
    void tearDown() throws Exception {
        if (sensor != null) {
            try {
                sensor.stop();
            } catch (Exception ignored) {}
        }
        if (serverSocket != null && !serverSocket.isClosed()) {
            serverSocket.close();
        }
    }

    // =========================================================================
    // scenario: sensor-init
    // =========================================================================

    /** init() must register the message output under the expected name. */
    @Test
    void initRegistersMessageOutput() throws Exception {
        sensor.init();

        assertNotNull(sensor.getOutputs().get(MeshtasticMessageOutput.OUTPUT_NAME),
                "init() must register a message output named '"
                        + MeshtasticMessageOutput.OUTPUT_NAME + "'");
    }

    /** init() must register the send-text-message control under the expected name. */
    @Test
    void initRegistersSendControl() throws Exception {
        sensor.init();

        assertNotNull(sensor.getCommandInputs().get(MeshtasticSendControl.CONTROL_NAME),
                "init() must register a control input named '"
                        + MeshtasticSendControl.CONTROL_NAME + "'");
    }

    // =========================================================================
    // scenario: sensor-connected-status
    // =========================================================================

    /** isConnected() must return false before start() is called. */
    @Test
    void isConnectedReturnsFalseBeforeStart() throws Exception {
        sensor.init();

        assertFalse(sensor.isConnected(),
                "isConnected() must be false before start() is called");
    }

    /**
     * After the driver successfully connects to the mock TCP server,
     * isConnected() must return true.
     */
    @Test
    void isConnectedReturnsTrueAfterSuccessfulConnection() throws Exception {
        CountDownLatch clientAccepted = new CountDownLatch(1);

        Thread server = startServer(() -> {
            try (Socket client = serverSocket.accept()) {
                clientAccepted.countDown();
                Thread.sleep(2_000); // keep connection alive during assertion
            } catch (Exception ignored) {}
        });

        sensor.init();
        sensor.start();

        assertTrue(clientAccepted.await(5, TimeUnit.SECONDS),
                "Mock server must accept the connection within 5 s");

        assertTrue(waitForConnected(sensor, 5_000),
                "isConnected() must return true after the driver establishes TCP connection");

        server.interrupt();
    }

    /**
     * When the driver successfully connects, it must fire a CONNECTED
     * {@link ModuleEvent} on the sensor's event bus.
     */
    @Test
    void startFiresConnectedModuleEventOnSuccess() throws Exception {
        CountDownLatch connectedLatch = new CountDownLatch(1);

        sensor.init();
        sensor.registerListener(event -> {
            if (event instanceof ModuleEvent me
                    && me.getType() == ModuleEvent.Type.CONNECTED) {
                connectedLatch.countDown();
            }
        });

        Thread server = startServer(() -> {
            try (Socket client = serverSocket.accept()) {
                Thread.sleep(2_000);
            } catch (Exception ignored) {}
        });

        sensor.start();

        assertTrue(connectedLatch.await(5, TimeUnit.SECONDS),
                "Driver must fire a CONNECTED module event when the TCP connection succeeds");

        server.interrupt();
    }

    // =========================================================================
    // scenario: sensor-receives-payload
    // =========================================================================

    /**
     * When the mock daemon sends a framed {@code FromRadio} packet containing a
     * text message, the driver must parse it and publish a {@link DataEvent} with
     * the correct field values.
     */
    @Test
    void receivedFromRadioPacketIsPublishedAsDataEvent() throws Exception {
        final String expectedText = "Unit test message";
        final int senderNode = 0xC0FFEE;

        CountDownLatch eventLatch = new CountDownLatch(1);
        AtomicReference<DataBlock> capturedBlock = new AtomicReference<>();

        sensor.init();
        sensor.getOutputs().get(MeshtasticMessageOutput.OUTPUT_NAME)
                .registerListener(event -> {
                    if (event instanceof DataEvent de) {
                        capturedBlock.set(de.getRecords()[0]);
                        eventLatch.countDown();
                    }
                });

        Thread server = startServer(() -> {
            try (Socket client = serverSocket.accept()) {
                writeFromRadioFrame(client.getOutputStream(), senderNode, expectedText);
                Thread.sleep(1_000);
            } catch (Exception ignored) {}
        });

        sensor.start();

        assertTrue(eventLatch.await(10, TimeUnit.SECONDS),
                "Driver must publish a DataEvent after receiving a FromRadio frame");

        DataBlock block = capturedBlock.get();
        assertNotNull(block, "Captured DataBlock must not be null");
        assertEquals(senderNode, block.getIntValue(1),
                "from_node (index 1) must match the sender node ID");
        assertEquals(expectedText, block.getStringValue(5),
                "payload_text (index 5) must match the sent text");

        server.interrupt();
    }

    /**
     * Multiple successive {@code FromRadio} packets must each result in a
     * separate {@link DataEvent} being published.
     */
    @Test
    void multipleReceivedFramesPublishSeparateDataEvents() throws Exception {
        final int frameCount = 3;

        CountDownLatch eventLatch = new CountDownLatch(frameCount);
        AtomicInteger eventsFired = new AtomicInteger(0);

        sensor.init();
        sensor.getOutputs().get(MeshtasticMessageOutput.OUTPUT_NAME)
                .registerListener(event -> {
                    if (event instanceof DataEvent) {
                        eventsFired.incrementAndGet();
                        eventLatch.countDown();
                    }
                });

        Thread server = startServer(() -> {
            try (Socket client = serverSocket.accept()) {
                OutputStream out = client.getOutputStream();
                for (int i = 0; i < frameCount; i++) {
                    writeFromRadioFrame(out, 0x100 + i, "Message " + i);
                }
                Thread.sleep(500);
            } catch (Exception ignored) {}
        });

        sensor.start();

        assertTrue(eventLatch.await(10, TimeUnit.SECONDS),
                "All " + frameCount + " frames must each produce a DataEvent");
        assertEquals(frameCount, eventsFired.get(),
                "Exactly " + frameCount + " DataEvents must be published");

        server.interrupt();
    }

    // =========================================================================
    // scenario: sensor-outgoing-message
    // =========================================================================

    /**
     * When a send-text-message command is submitted, the driver must write a
     * valid framed {@code ToRadio} message to the TCP connection.
     */
    @Test
    void sendControlTransmitsFramedToRadioToDaemon() throws Exception {
        final String sentText = "Hello from OSH";
        final int targetChannel = 2;

        BlockingQueue<byte[]> receivedPayloads = new ArrayBlockingQueue<>(5);

        Thread server = startServer(() -> {
            try (Socket client = serverSocket.accept()) {
                InputStream in = client.getInputStream();
                byte[] header = new byte[8];
                readFully(in, header, 8);
                int length = ByteBuffer.wrap(header, 4, 4).getInt();
                byte[] payload = new byte[length];
                readFully(in, payload, length);
                receivedPayloads.offer(payload);
                Thread.sleep(500);
            } catch (Exception ignored) {}
        });

        sensor.init();
        sensor.start();

        assertTrue(waitForOutputStream(sensor, 5_000),
                "Driver must establish TCP connection within 5 s");

        MeshtasticSendControl control = (MeshtasticSendControl)
                sensor.getCommandInputs().get(MeshtasticSendControl.CONTROL_NAME);
        assertNotNull(control, "sendTextMessage control must be registered");

        DataBlock cmd = control.getCommandDescription().createDataBlock();
        cmd.setIntValue(0, 0);            // broadcast
        cmd.setIntValue(1, targetChannel);
        cmd.setStringValue(2, sentText);
        control.execCommand(cmd);

        byte[] payload = receivedPayloads.poll(5, TimeUnit.SECONDS);
        assertNotNull(payload, "Mock daemon must receive a ToRadio frame within 5 s");

        ToRadio toRadio = ToRadio.parseFrom(payload);
        assertTrue(toRadio.hasPacket(), "ToRadio must contain a MeshPacket");
        assertEquals(targetChannel, toRadio.getPacket().getChannel(),
                "Channel field must match the submitted command");
        assertEquals(sentText,
                toRadio.getPacket().getDecoded().getPayload().toStringUtf8(),
                "Payload text must match the submitted text");

        server.interrupt();
    }

    /**
     * When the send control is used with to_node=0, the driver must set the
     * destination field to {@code 0xFFFFFFFF} (broadcast address).
     */
    @Test
    void sendControlSetsBroadcastAddressForToNodeZero() throws Exception {
        BlockingQueue<byte[]> receivedPayloads = new ArrayBlockingQueue<>(5);

        Thread server = startServer(() -> {
            try (Socket client = serverSocket.accept()) {
                InputStream in = client.getInputStream();
                byte[] header = new byte[8];
                readFully(in, header, 8);
                int length = ByteBuffer.wrap(header, 4, 4).getInt();
                byte[] payload = new byte[length];
                readFully(in, payload, length);
                receivedPayloads.offer(payload);
                Thread.sleep(500);
            } catch (Exception ignored) {}
        });

        sensor.init();
        sensor.start();

        assertTrue(waitForOutputStream(sensor, 5_000),
                "Driver must connect within 5 s");

        MeshtasticSendControl control = (MeshtasticSendControl)
                sensor.getCommandInputs().get(MeshtasticSendControl.CONTROL_NAME);
        DataBlock cmd = control.getCommandDescription().createDataBlock();
        cmd.setIntValue(0, 0);   // to_node = 0 → broadcast
        cmd.setIntValue(1, 0);
        cmd.setStringValue(2, "broadcast");
        control.execCommand(cmd);

        byte[] payload = receivedPayloads.poll(5, TimeUnit.SECONDS);
        assertNotNull(payload, "Payload must arrive at mock daemon");

        ToRadio toRadio = ToRadio.parseFrom(payload);
        assertEquals(0xFFFFFFFFL, Integer.toUnsignedLong(toRadio.getPacket().getTo()),
                "to_node 0 must be expanded to 0xFFFFFFFF (broadcast)");

        server.interrupt();
    }

    // =========================================================================
    // scenario: sensor-failure-unreachable
    // =========================================================================

    /**
     * When the daemon address is unreachable, isConnected() must remain false
     * and no exception must propagate out of start().
     */
    @Test
    void isConnectedRemainseFalseWhenAddressIsUnreachable() throws Exception {
        serverSocket.close(); // nothing listening on the port

        sensor.init();
        sensor.start(); // must not throw

        Thread.sleep(200); // give the reader thread time to attempt and fail

        assertFalse(sensor.isConnected(),
                "isConnected() must be false when no daemon is reachable");
    }

    /**
     * When the daemon address is unreachable, start() must not throw an
     * exception — the driver must handle the failure internally.
     */
    @Test
    void startDoesNotThrowWhenAddressIsUnreachable() throws Exception {
        serverSocket.close();

        sensor.init();

        assertDoesNotThrow(() -> sensor.start(),
                "start() must not throw even when the daemon address is unreachable");
    }

    /**
     * When the daemon address is unreachable, the driver must fire a DISCONNECTED
     * {@link ModuleEvent} after the failed connection attempt.
     */
    @Test
    void disconnectedEventFiredWhenAddressIsUnreachable() throws Exception {
        serverSocket.close();

        CountDownLatch disconnectedLatch = new CountDownLatch(1);

        sensor.init();
        sensor.registerListener(event -> {
            if (event instanceof ModuleEvent me
                    && me.getType() == ModuleEvent.Type.DISCONNECTED) {
                disconnectedLatch.countDown();
            }
        });

        sensor.start();

        assertTrue(disconnectedLatch.await(10, TimeUnit.SECONDS),
                "Driver must fire a DISCONNECTED event when the daemon is unreachable");
    }

    // =========================================================================
    // scenario: sensor-stop
    // =========================================================================

    /** After stop() is called, isConnected() must return false. */
    @Test
    void isConnectedReturnsFalseAfterStop() throws Exception {
        CountDownLatch connected = new CountDownLatch(1);

        Thread server = startServer(() -> {
            try (Socket client = serverSocket.accept()) {
                connected.countDown();
                Thread.sleep(5_000); // keep alive while driver is running
            } catch (Exception ignored) {}
        });

        sensor.init();
        sensor.start();

        assertTrue(connected.await(5, TimeUnit.SECONDS),
                "Driver must connect before stop() is tested");

        sensor.stop();

        assertFalse(sensor.isConnected(),
                "isConnected() must return false after stop()");

        server.interrupt();
    }

    /**
     * Calling stop() on an initialised-but-not-started sensor must not throw.
     */
    @Test
    void stopBeforeStartDoesNotThrow() throws Exception {
        sensor.init();

        assertDoesNotThrow(() -> sensor.stop(),
                "stop() before start() must not throw");
    }

    /**
     * After stop(), the driver must fire a DISCONNECTED {@link ModuleEvent}.
     */
    @Test
    void stopFiresDisconnectedModuleEvent() throws Exception {
        CountDownLatch disconnectedLatch = new CountDownLatch(1);

        Thread server = startServer(() -> {
            try (Socket client = serverSocket.accept()) {
                Thread.sleep(5_000);
            } catch (Exception ignored) {}
        });

        sensor.init();
        sensor.registerListener(event -> {
            if (event instanceof ModuleEvent me
                    && me.getType() == ModuleEvent.Type.DISCONNECTED) {
                disconnectedLatch.countDown();
            }
        });
        sensor.start();

        assertTrue(waitForConnected(sensor, 5_000),
                "Driver must connect before stop() is tested");

        sensor.stop();

        assertTrue(disconnectedLatch.await(5, TimeUnit.SECONDS),
                "stop() must cause a DISCONNECTED module event to be fired");

        server.interrupt();
    }

    // =========================================================================
    // Private helpers
    // =========================================================================

    /**
     * Starts a daemon thread that executes {@code body} and returns it.
     */
    private Thread startServer(RunnableWithException body) {
        Thread t = new Thread(() -> {
            try {
                body.run();
            } catch (Exception ignored) {}
        }, "mock-tcp-server");
        t.setDaemon(true);
        t.start();
        return t;
    }

    /**
     * Writes a framed {@code FromRadio} text-message packet to {@code out}.
     *
     * @param out        the server-side output stream (sent to the driver)
     * @param senderNode the simulated sender node ID
     * @param text       the text message body
     */
    private static void writeFromRadioFrame(OutputStream out, int senderNode, String text)
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
                .setRxSnr(5.0f)
                .setRxRssi(-95)
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
     * Reads exactly {@code len} bytes from {@code in} into {@code buf}.
     *
     * @throws IOException if the stream ends prematurely
     */
    private static void readFully(InputStream in, byte[] buf, int len) throws IOException {
        int read = 0;
        while (read < len) {
            int n = in.read(buf, read, len - read);
            if (n == -1) {
                throw new IOException("Stream closed after " + read + " of " + len + " bytes");
            }
            read += n;
        }
    }

    /**
     * Polls until {@link MeshtasticSensor#isConnected()} returns true or
     * {@code timeoutMs} milliseconds elapse.
     *
     * @return true if the driver reported connected within the timeout
     */
    private static boolean waitForConnected(MeshtasticSensor sensor, long timeoutMs)
            throws InterruptedException {
        long deadline = System.currentTimeMillis() + timeoutMs;
        while (!sensor.isConnected() && System.currentTimeMillis() < deadline) {
            Thread.sleep(50);
        }
        return sensor.isConnected();
    }

    /**
     * Polls until the sensor's output stream becomes non-null (i.e. connected).
     *
     * @return true if the output stream is available within the timeout
     */
    private static boolean waitForOutputStream(MeshtasticSensor sensor, long timeoutMs)
            throws InterruptedException {
        long deadline = System.currentTimeMillis() + timeoutMs;
        while (sensor.getOutputStream() == null && System.currentTimeMillis() < deadline) {
            Thread.sleep(50);
        }
        return sensor.getOutputStream() != null;
    }

    /**
     * Functional interface for server body lambdas that may throw checked exceptions.
     */
    @FunctionalInterface
    private interface RunnableWithException {
        void run() throws Exception;
    }
}