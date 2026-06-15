package org.sensorhub.impl.sensor.mavsdk.cs;

import org.sensorhub.impl.sensor.AbstractSensorControl;
import org.sensorhub.impl.sensor.mavsdk.MavSdkDriver;
import net.opengis.swe.v20.DataBlock;
import net.opengis.swe.v20.DataComponent;
import net.opengis.swe.v20.DataRecord;
import org.sensorhub.api.command.CommandException;
import io.mavsdk.System;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.vast.swe.SWEHelper;

public class CSControlHandler {
    private static final Logger log = LoggerFactory.getLogger(CSControlHandler.class);
    private final MavSdkDriver driver;
    private final System system;
    private TakeoffControl takeoffControl;

    public CSControlHandler(MavSdkDriver driver, System system) {
        this.driver = driver;
        this.system = system;
    }

    public void start() {
        log.info("Starting CSControlHandler for ControlStreams");
        takeoffControl = new TakeoffControl(driver, system);
        driver.registerControl(takeoffControl);
    }

    public void stop() {
        log.info("Stopping CSControlHandler");
        // Module level stop clears all controls
    }

    private static class TakeoffControl extends AbstractSensorControl<MavSdkDriver> {
        private final System system;
        private final DataRecord commandDesc;

        public TakeoffControl(MavSdkDriver parent, System system) {
            super("Takeoff", parent);
            this.system = system;
            SWEHelper fac = new SWEHelper();
            commandDesc = fac.newDataRecord(4);
            commandDesc.setName("Takeoff");
            commandDesc.setDescription("Commands the vehicle to takeoff");
        }

        @Override
        public DataComponent getCommandDescription() {
            return commandDesc;
        }

        @Override
        protected boolean execCommand(DataBlock cmdData) throws CommandException {
            try {
                system.getAction().takeoff().blockingAwait();
                return true;
            } catch (Exception e) {
                getLogger().error("Takeoff command failed", e);
                throw new CommandException("Takeoff command failed", e);
            }
        }
    }
}
