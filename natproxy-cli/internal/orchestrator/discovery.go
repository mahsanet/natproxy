package orchestrator

import (
	"errors"
	"time"

	"natproxy/golib/applog"
	"natproxy/golib/signaling"
)

const heartbeatInterval = 45 * time.Second

// HeartbeatParams holds the registration parameters needed to re-register
// when a listing expires on the signaling server.
type HeartbeatParams struct {
	SigURL         string
	Name           string
	Room           string
	ConnectionCode string
	Method         string
	Transport      string
	Protocol       string
}

func RegisterDiscovery(sigURL, name, room, connectionCode, method, transport, protocol string) (string, error) {
	return signaling.RegisterServer(sigURL, name, room, connectionCode, method, transport, protocol)
}

// StartHeartbeat sends periodic heartbeats and re-registers automatically
// when the listing expires (404). onNewID is called when re-registration
// produces a new listing ID.
func StartHeartbeat(params HeartbeatParams, listingID string, stop <-chan struct{}, onNewID func(string)) {
	currentID := listingID
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			err := signaling.HeartbeatServer(params.SigURL, currentID)
			if err == nil {
				continue
			}
			if errors.Is(err, signaling.ErrListingExpired) {
				applog.Warnf("Discovery listing expired, re-registering...")
				newID, regErr := signaling.RegisterServer(
					params.SigURL, params.Name, params.Room,
					params.ConnectionCode, params.Method, params.Transport, params.Protocol,
				)
				if regErr != nil {
					applog.Warnf("Discovery re-registration failed: %v", regErr)
					continue
				}
				currentID = newID
				if onNewID != nil {
					onNewID(newID)
				}
				applog.Infof("Re-registered on discovery with ID: %s", newID)
			} else {
				applog.Warnf("Discovery heartbeat failed: %v", err)
			}
		}
	}
}

func UnregisterDiscovery(sigURL, listingID string) error {
	return signaling.DeregisterServer(sigURL, listingID)
}

func ListServers(sigURL, room string) (string, error) {
	if err := validateSignalingURL(sigURL); err != nil {
		return "", err
	}
	return signaling.ListServers(sigURL, room)
}
