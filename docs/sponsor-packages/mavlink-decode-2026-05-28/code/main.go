package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/minimal"
)

type HeartbeatResponse struct {
	SystemID      byte      `json:"system_id"`
	ComponentID   byte      `json:"component_id"`
	AutopilotType int       `json:"autopilot_type"`
	BaseMode      int       `json:"base_mode"`
	ReceivedAt    time.Time `json:"received_at"`
}

var (
	mu            sync.RWMutex
	lastHeartbeat *HeartbeatResponse
)

func handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mu.RLock()
	hb := lastHeartbeat
	mu.RUnlock()

	if hb == nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "no heartbeat received"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(hb); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

func startMavlinkListener(address string) (*gomavlib.Node, error) {
	node, err := gomavlib.NewNode(gomavlib.NodeConf{
		Endpoints: []gomavlib.EndpointConf{
			gomavlib.EndpointUDPServer{Address: address},
		},
		Dialect:     minimal.Dialect,
		OutVersion:  gomavlib.V2,
		OutSystemID: 254,
	})
	if err != nil {
		return nil, err
	}

	go func() {
		for evt := range node.Events() {
			if frm, ok := evt.(*gomavlib.EventFrame); ok {
				if msg, ok := frm.Message().(*minimal.MessageHeartbeat); ok {
					mu.Lock()
					lastHeartbeat = &HeartbeatResponse{
						SystemID:      frm.SystemID(),
						ComponentID:   frm.ComponentID(),
						AutopilotType: int(msg.Autopilot),
						BaseMode:      int(msg.BaseMode),
						ReceivedAt:    time.Now().UTC(),
					}
					mu.Unlock()
				}
			}
		}
	}()

	return node, nil
}

func startHTTPServer(address string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/heartbeat", handleHeartbeat)
	
	server := &http.Server{
		Addr:    address,
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()
	
	return server
}

func main() {
	node, err := startMavlinkListener(":14540")
	if err != nil {
		log.Fatalf("Failed to start MAVLink node: %v", err)
	}
	defer node.Close()

	server := startHTTPServer(":8080")
	defer server.Close()
	
	log.Println("MAVLink listener on :14540, HTTP server on :8080")
	select {} // Block forever
}