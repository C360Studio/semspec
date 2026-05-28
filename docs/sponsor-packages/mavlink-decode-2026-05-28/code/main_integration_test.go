//go:build integration

package main

import (
	"log"
	"testing"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/minimal"
)

func TestIntegration_DecodeHeartbeatUDP(t *testing.T) {
	mu.Lock()
	lastHeartbeat = nil
	mu.Unlock()

	node, err := startMavlinkListener("127.0.0.1:14540")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer node.Close()

	time.Sleep(100 * time.Millisecond)

	clientNode, err := gomavlib.NewNode(gomavlib.NodeConf{
		Endpoints: []gomavlib.EndpointConf{
			gomavlib.EndpointUDPClient{Address: "127.0.0.1:14540"},
		},
		Dialect:     minimal.Dialect,
		OutVersion:  gomavlib.V2,
		OutSystemID: 42,
		OutComponentID: 43,
	})
	if err != nil {
		t.Fatalf("failed to start client: %v", err)
	}
	defer clientNode.Close()

	// Wait for channel to open
	go func() {
		for e := range clientNode.Events() {
			log.Printf("Client Event: %T", e)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	err = clientNode.WriteMessageAll(&minimal.MessageHeartbeat{
		Type:           minimal.MAV_TYPE_QUADROTOR,
		Autopilot:      minimal.MAV_AUTOPILOT_PX4,
		BaseMode:       minimal.MAV_MODE_FLAG_SAFETY_ARMED,
		CustomMode:     0,
		SystemStatus:   minimal.MAV_STATE_ACTIVE,
		MavlinkVersion: 3,
	})
	if err != nil {
		t.Fatalf("failed to write heartbeat: %v", err)
	}

	var hb *HeartbeatResponse
	for i := 0; i < 50; i++ {
		time.Sleep(20 * time.Millisecond)
		mu.RLock()
		hb = lastHeartbeat
		mu.RUnlock()
		if hb != nil {
			break
		}
	}

	if hb == nil {
		t.Fatal("timed out waiting for heartbeat to be stored")
	}

	if hb.SystemID != 42 {
		t.Errorf("got systemID %d, want 42", hb.SystemID)
	}
	if hb.ComponentID != 43 {
		t.Errorf("got componentID %d, want 43", hb.ComponentID)
	}
}