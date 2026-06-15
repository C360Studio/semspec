package org.sensorhub.impl.sensor.mavsdk.cs;

import org.sensorhub.impl.sensor.mavsdk.MavSdkDriver;
import io.mavsdk.System;
import io.reactivex.disposables.Disposable;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

public class CSTelemetryHandler {
    private static final Logger log = LoggerFactory.getLogger(CSTelemetryHandler.class);
    private final MavSdkDriver driver;
    private final System system;
    private Disposable positionDisposable;

    public CSTelemetryHandler(MavSdkDriver driver, System system) {
        this.driver = driver;
        this.system = system;
    }

    public void start() {
        log.info("Starting CSTelemetryHandler for DataStreams and Observations");
        if (system != null) {
            positionDisposable = system.getTelemetry().getPosition().subscribe(pos -> {
                if (driver.getLocationOutput() != null) {
                    driver.getLocationOutput().updateLocation(
                        java.lang.System.currentTimeMillis() / 1000.0,
                        pos.getLongitudeDeg(),
                        pos.getLatitudeDeg(),
                        pos.getAbsoluteAltitudeM(),
                        false
                    );
                }
            }, error -> {
                log.error("Error in telemetry position stream", error);
            });
        }
    }

    public void stop() {
        log.info("Stopping CSTelemetryHandler");
        if (positionDisposable != null) {
            positionDisposable.dispose();
        }
    }
}
