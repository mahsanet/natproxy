package webrtc

import (
	"sync"

	"natproxy/golib/applog"
)

// NetworkMonitor manages callbacks for network change events from the platform layer.
var globalNetMon = &networkMonitor{}

type networkMonitor struct {
	mu        sync.RWMutex
	callbacks []func()
	lastNet   string
}

// OnNetworkChanged registers a callback that fires when the network type changes.
func OnNetworkChanged(cb func()) {
	globalNetMon.mu.Lock()
	defer globalNetMon.mu.Unlock()
	globalNetMon.callbacks = append(globalNetMon.callbacks, cb)
}

// ClearNetworkCallbacks removes all registered callbacks.
func ClearNetworkCallbacks() {
	globalNetMon.mu.Lock()
	defer globalNetMon.mu.Unlock()
	globalNetMon.callbacks = nil
}

// NotifyNetworkChange is called from the platform layer (Android/iOS) when
// the network type changes (e.g., WiFi → cellular, lost, available).
// It fires all registered callbacks if the network type actually changed.
func NotifyNetworkChange(networkType string) {
	globalNetMon.mu.Lock()
	if networkType == globalNetMon.lastNet {
		globalNetMon.mu.Unlock()
		return
	}
	applog.Infof("netmon: network changed: %s → %s", globalNetMon.lastNet, networkType)
	globalNetMon.lastNet = networkType
	cbs := make([]func(), len(globalNetMon.callbacks))
	copy(cbs, globalNetMon.callbacks)
	globalNetMon.mu.Unlock()

	for _, cb := range cbs {
		go cb()
	}
}
