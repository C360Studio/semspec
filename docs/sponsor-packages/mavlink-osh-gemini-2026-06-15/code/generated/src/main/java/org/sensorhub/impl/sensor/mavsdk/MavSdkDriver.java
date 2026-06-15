package org.sensorhub.impl.sensor.mavsdk;

import org.sensorhub.impl.sensor.AbstractSensorModule;
import org.sensorhub.api.common.SensorHubException;
import io.mavsdk.System;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import io.reactivex.disposables.Disposable;
import org.sensorhub.impl.sensor.mavsdk.cs.CSTelemetryHandler;
import org.sensorhub.impl.sensor.mavsdk.cs.CSControlHandler;
import org.sensorhub.impl.sensor.mavsdk.cs.CSGenericMavlinkHandler;

public class MavSdkDriver extends AbstractSensorModule<UnmannedConfig> {
    private static final Logger log = LoggerFactory.getLogger(MavSdkDriver.class);

    private MavSdkServerHandler serverHandler;
    private System mavsdkSystem;
    private boolean initialized = false;
    private boolean connected = false;
    private Disposable connectionStateDisposable;
    private CSTelemetryHandler telemetryHandler;
    private CSControlHandler controlHandler;
    private CSGenericMavlinkHandler genericMavlinkHandler;
    private RawMavlinkBridge rawMavlinkBridge;

    @Override
    protected void doInit() throws SensorHubException {
        if (config == null || config.connectionString == null) {
            throw new SensorHubException("connectionString is required in config");
        }
        serverHandler = new MavSdkServerHandler(config.connectionString);
        
        // Register location output for telemetry
        addLocationOutput(1.0);
        
        initialized = true;
    }

    @Override
    protected void doStart() throws SensorHubException {
        try {
            serverHandler.start();
            mavsdkSystem = new System();
            
            telemetryHandler = new CSTelemetryHandler(this, mavsdkSystem);
            telemetryHandler.start();

            controlHandler = new CSControlHandler(this, mavsdkSystem);
            controlHandler.start();

            genericMavlinkHandler = new CSGenericMavlinkHandler(this, mavsdkSystem);
            genericMavlinkHandler.start();

            rawMavlinkBridge = new RawMavlinkBridge(this, config.connectionString);
            rawMavlinkBridge.start();
            
            connectionStateDisposable = mavsdkSystem.getCore().getConnectionState().subscribe(state -> {
                connected = state.getIsConnected();
                if (connected) {
                    log.info("MAVLink connected");
                } else {
                    log.info("MAVLink disconnected");
                }
            }, error -> {
                log.error("Error in connection state stream", error);
            });
            
        } catch (Exception e) {
            throw new SensorHubException("Failed to start MAVSDK driver", e);
        }
    }

    @Override
    protected void doStop() throws SensorHubException {
        if (telemetryHandler != null) telemetryHandler.stop();
        if (controlHandler != null) controlHandler.stop();
        if (genericMavlinkHandler != null) genericMavlinkHandler.stop();
        if (rawMavlinkBridge != null) rawMavlinkBridge.stop();
        
        if (connectionStateDisposable != null) {
            connectionStateDisposable.dispose();
        }
        connected = false;
        if (mavsdkSystem != null) {
            mavsdkSystem.dispose();
            mavsdkSystem = null;
        }
        if (serverHandler != null) {
            serverHandler.stop();
        }
    }

    public void registerControl(org.sensorhub.api.command.IStreamingControlInterface ctrl) {
        addControlInput(ctrl);
    }
    
    @Override
    public boolean isConnected() {
        return connected;
    }

    public org.sensorhub.impl.sensor.DefaultLocationOutput getLocationOutput() { return locationOutput; }

    // For testing
    public System getMavsdkSystem() { return mavsdkSystem; }

    public RawMavlinkBridge getRawMavlinkBridge() { return rawMavlinkBridge; }

    MavSdkServerHandler getServerHandler() {
        return serverHandler;
    }
}
