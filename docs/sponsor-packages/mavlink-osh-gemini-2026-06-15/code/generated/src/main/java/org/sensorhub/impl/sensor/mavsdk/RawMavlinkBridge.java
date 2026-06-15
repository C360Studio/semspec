package org.sensorhub.impl.sensor.mavsdk;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.InetAddress;
import java.net.SocketException;
import java.net.URI;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.io.ByteArrayInputStream;
import javax.xml.parsers.DocumentBuilder;
import javax.xml.parsers.DocumentBuilderFactory;
import org.w3c.dom.Document;
import org.w3c.dom.Element;
import org.w3c.dom.NodeList;
import java.util.List;
import java.util.ArrayList;

public class RawMavlinkBridge {
    private static final Logger log = LoggerFactory.getLogger(RawMavlinkBridge.class);
    
    private final MavSdkDriver driver;
    private final String connectionString;
    private DatagramSocket socket;
    private ExecutorService receiveExecutor;
    private boolean running = false;
    private int port = 14550; // default MAVLink fallback port
    
    // Custom XML dialect storage: messageId -> messageName
    private final Map<Integer, String> customDialect = new ConcurrentHashMap<>();
    
    // For testing/routing
    private final List<RoutedMessage> routedMessages = new ArrayList<>();

    public static class RoutedMessage {
        public final int msgId;
        public final String name;
        public final byte[] payload;
        public RoutedMessage(int msgId, String name, byte[] payload) {
            this.msgId = msgId;
            this.name = name;
            this.payload = payload;
        }
    }

    public RawMavlinkBridge(MavSdkDriver driver, String connectionString) {
        this.driver = driver;
        this.connectionString = connectionString;
        parseConnectionString();
    }
    
    private void parseConnectionString() {
        if (connectionString == null || !connectionString.startsWith("udp://")) return;
        try {
            URI uri = new URI(connectionString);
            if (uri.getPort() > 0) {
                this.port = uri.getPort();
            }
        } catch (Exception e) {
            log.warn("Invalid connection string format: {}", connectionString);
        }
    }

    public void start() throws Exception {
        log.info("Starting RawMavlinkBridge on port {}", port);
        try {
            socket = new DatagramSocket(port);
            running = true;
            receiveExecutor = Executors.newSingleThreadExecutor();
            receiveExecutor.submit(this::receiveLoop);
        } catch (SocketException e) {
            log.error("Failed to bind RawMavlinkBridge to port {}", port, e);
            throw new Exception("Could not start RawMavlinkBridge", e);
        }
    }
    
    public void loadXmlDialect(String xmlContent) throws Exception {
        DocumentBuilderFactory factory = DocumentBuilderFactory.newInstance();
        DocumentBuilder builder = factory.newDocumentBuilder();
        Document doc = builder.parse(new ByteArrayInputStream(xmlContent.getBytes()));
        
        NodeList messages = doc.getElementsByTagName("message");
        for (int i = 0; i < messages.getLength(); i++) {
            Element msgElement = (Element) messages.item(i);
            int id = Integer.parseInt(msgElement.getAttribute("id"));
            String name = msgElement.getAttribute("name");
            customDialect.put(id, name);
        }
        log.info("Loaded custom XML dialect with {} messages", customDialect.size());
    }

    private void receiveLoop() {
        byte[] buffer = new byte[2048];
        while (running && !Thread.currentThread().isInterrupted()) {
            try {
                DatagramPacket packet = new DatagramPacket(buffer, buffer.length);
                socket.receive(packet);
                handleRawPacket(packet.getData(), packet.getLength());
            } catch (Exception e) {
                if (running) {
                    log.error("Error receiving MAVLink packet", e);
                }
            }
        }
    }

    protected void handleRawPacket(byte[] data, int length) {
        if (length < 8) return; // Too short for any MAVLink
        
        int magic = data[0] & 0xFF;
        int msgId = -1;
        int payloadLen = data[1] & 0xFF;
        byte[] payload = null;
        
        if (magic == 0xFE) { // MAVLink 1
            if (length < 8 + payloadLen) return;
            msgId = data[5] & 0xFF;
            payload = new byte[payloadLen];
            System.arraycopy(data, 6, payload, 0, payloadLen);
        } else if (magic == 0xFD) { // MAVLink 2
            if (length < 12 + payloadLen) return;
            msgId = (data[7] & 0xFF) | ((data[8] & 0xFF) << 8) | ((data[9] & 0xFF) << 16);
            payload = new byte[payloadLen];
            System.arraycopy(data, 10, payload, 0, payloadLen);
        } else {
            // Not a recognized MAVLink magic
            return;
        }
        
        String msgName = customDialect.getOrDefault(msgId, "UNKNOWN_MSG");
        log.debug("Routed raw MAVLink message: id={}, name={}", msgId, msgName);
        
        synchronized (routedMessages) {
            routedMessages.add(new RoutedMessage(msgId, msgName, payload));
        }
    }
    
    public List<RoutedMessage> getRoutedMessages() {
        synchronized (routedMessages) {
            return new ArrayList<>(routedMessages);
        }
    }

    public void sendRawPacket(byte[] data) throws Exception {
        if (socket == null || !running) {
            throw new IllegalStateException("RawMavlinkBridge is not running");
        }
        InetAddress target = InetAddress.getByName("127.0.0.1");
        DatagramPacket packet = new DatagramPacket(data, data.length, target, 14540); // default SITL
        socket.send(packet);
    }

    public void stop() {
        log.info("Stopping RawMavlinkBridge");
        running = false;
        if (socket != null) {
            socket.close();
        }
        if (receiveExecutor != null) {
            receiveExecutor.shutdownNow();
        }
    }

    public boolean isRunning() {
        return running;
    }
    
    public Map<Integer, String> getCustomDialect() {
        return customDialect;
    }
}
