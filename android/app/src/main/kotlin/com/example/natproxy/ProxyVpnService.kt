package com.example.natproxy

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.content.pm.ServiceInfo
import android.graphics.drawable.Icon
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.util.Log
import org.json.JSONObject

class ProxyVpnService : VpnService() {
    companion object {
        private const val TAG = "ProxyVpnService"
        private const val NOTIFICATION_ID = 1
        private const val CHANNEL_ID = "natproxy_vpn"
        const val ACTION_STOP = "com.example.natproxy.action.STOP_CLIENT"
        var instance: ProxyVpnService? = null
    }

    private var tunInterface: ParcelFileDescriptor? = null
    private var networkCallback: ConnectivityManager.NetworkCallback? = null

    override fun onCreate() {
        super.onCreate()
        instance = this
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            Log.i(TAG, "Disconnect requested from notification")
            MainActivity.sendStopEvent("client")
            shutdown()
            return START_NOT_STICKY
        }

        val mode = intent?.getStringExtra("mode") ?: "legacy"
        val settingsJson = intent?.getStringExtra("settings_json") ?: "{}"

        // Parse VPN-specific settings from JSON
        val settings =
                try {
                    JSONObject(settingsJson)
                } catch (_: Exception) {
                    JSONObject()
                }
        val tunAddress = settings.optString("tunAddress", "10.0.0.2")
        val mtu = settings.optInt("mtu", 1500)
        val dns1 = settings.optString("dns1", "8.8.8.8")
        val dns2 = settings.optString("dns2", "1.1.1.1")
        val vpnSessionName = settings.optString("vpnSessionName", "NATProxy")

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(NOTIFICATION_ID, buildNotification(), ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(NOTIFICATION_ID, buildNotification())
        }

        try {
            // Close previous TUN interface if service is re-entered
            tunInterface?.let {
                Log.w(TAG, "onStartCommand: closing stale TUN interface from previous connection")
                try { it.close() } catch (_: Exception) {}
                tunInterface = null
            }

            val builder =
                    Builder()
                            .setSession(vpnSessionName)
                            .addAddress(tunAddress, 32)
                            .addRoute("0.0.0.0", 0)
                            .addRoute("::", 0)
                            .addDnsServer(dns1)
                            .addDnsServer(dns2)
                            .setMtu(mtu)
                            .setBlocking(false)

            tunInterface =
                    builder.establish() ?: throw IllegalStateException("VPN permission not granted")

            val fd = tunInterface!!.fd

            if (mode == "tunnel") {
                // Two-phase mode: WebRTC already connected by ConnectWebRTC.
                // Protect sockets and start TUN routing.
                Thread {
                    try {
                        GoBridge.startTunnel(fd, settingsJson) { socketFd ->
                            protect(socketFd)
                        }
                        Log.i(TAG, "TUN tunnel started (tun=$tunAddress, mtu=$mtu, dns=$dns1/$dns2)")
                        val reason = GoBridge.waitClientDisconnect()
                        Log.i(TAG, "Client disconnected: $reason")
                    } catch (e: Throwable) {
                        Log.e(TAG, "Failed to start tunnel", e)
                    }
                    MainActivity.sendStopEvent("client")
                    shutdown()
                }.start()
            } else {
                // Legacy mode: full connection flow (startClient does everything)
                val connectionCode = intent?.getStringExtra("connection_code") ?: run {
                    stopSelf()
                    return START_NOT_STICKY
                }
                Thread {
                    try {
                        GoBridge.startClient(connectionCode, fd, settingsJson) { socketFd ->
                            protect(socketFd)
                        }
                        Log.i(TAG, "VPN tunnel established (tun=$tunAddress, mtu=$mtu, dns=$dns1/$dns2)")
                        val reason = GoBridge.waitClientDisconnect()
                        Log.i(TAG, "Client disconnected: $reason")
                    } catch (e: Throwable) {
                        Log.e(TAG, "Failed to start Go client", e)
                    }
                    MainActivity.sendStopEvent("client")
                    shutdown()
                }.start()
            }

            registerNetworkCallback()
        } catch (e: Exception) {
            Log.e(TAG, "Failed to establish VPN", e)
            stopSelf()
            return START_NOT_STICKY
        }

        return START_NOT_STICKY
    }

    override fun onRevoke() {
        Log.i(TAG, "VPN revoked by user")
        stopTunnel()
        stopSelf()
    }

    override fun onDestroy() {
        stopTunnel()
        instance = null
        super.onDestroy()
    }

    // Tears down everything — Go bridge, TUN fd, then the service itself.
    fun shutdown() {
        try {
            stopTunnel()
        } finally {
            stopSelf()
        }
    }

    private fun registerNetworkCallback() {
        val cm = getSystemService(ConnectivityManager::class.java) ?: return
        val cb = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                GoBridge.onNetworkChanged("available")
            }

            override fun onLost(network: Network) {
                GoBridge.onNetworkChanged("lost")
            }

            override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
                val type = when {
                    caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
                    caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cellular"
                    caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "ethernet"
                    else -> "other"
                }
                GoBridge.onNetworkChanged(type)
            }
        }
        networkCallback = cb
        cm.registerDefaultNetworkCallback(cb)
        Log.i(TAG, "Network change callback registered")
    }

    private fun unregisterNetworkCallback() {
        val cb = networkCallback ?: return
        networkCallback = null
        try {
            val cm = getSystemService(ConnectivityManager::class.java)
            cm?.unregisterNetworkCallback(cb)
            Log.i(TAG, "Network change callback unregistered")
        } catch (e: Exception) {
            Log.w(TAG, "Error unregistering network callback: ${e.message}")
        }
    }

    private fun stopTunnel() {
        Log.i(TAG, "stopTunnel: cleaning up...")

        unregisterNetworkCallback()

        // Close dup'd PFD
        try {
            GoBridge.stopAll()
            Log.i(TAG, "stopTunnel: Go bridge stopped")
        } catch (e: Exception) {
            Log.e(TAG, "Error stopping Go bridge", e)
        }

        // Close the original PFD from establish()
        try {
            tunInterface?.close()
            Log.i(TAG, "stopTunnel: TUN interface closed")
        } catch (e: Exception) {
            Log.e(TAG, "Error closing TUN interface", e)
        }
        tunInterface = null
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
            stopForeground(STOP_FOREGROUND_REMOVE)
        } else {
            @Suppress("DEPRECATION")
            stopForeground(true)
        }
        Log.i(TAG, "stopTunnel: foreground stopped, cleanup complete")
    }

    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel =
                    NotificationChannel(
                                    CHANNEL_ID,
                                    "NATProxy",
                                    NotificationManager.IMPORTANCE_LOW
                            )
                            .apply {
                                description = "Connection status and controls"
                                setShowBadge(false)
                            }
            val manager = getSystemService(NotificationManager::class.java)
            manager.createNotificationChannel(channel)
        }
    }

    private fun buildNotification(): Notification {
        // Tap notification → open app
        val openIntent = Intent(this, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
        }
        val openPendingIntent = PendingIntent.getActivity(
            this, 0, openIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )

        // Disconnect button
        val stopIntent = Intent(this, ProxyVpnService::class.java).apply {
            action = ACTION_STOP
        }
        val stopPendingIntent = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            PendingIntent.getForegroundService(
                this, 2, stopIntent,
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
            )
        } else {
            PendingIntent.getService(
                this, 2, stopIntent,
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
            )
        }

        val disconnectAction = Notification.Action.Builder(
            Icon.createWithResource(this, android.R.drawable.ic_menu_close_clear_cancel),
            "Disconnect",
            stopPendingIntent
        ).build()

        val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(this, CHANNEL_ID)
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(this)
        }

        return builder
            .setContentTitle("NATProxy VPN")
            .setContentText("Routing traffic through tunnel")
            .setSmallIcon(android.R.drawable.ic_dialog_info)
            .setOngoing(true)
            .setContentIntent(openPendingIntent)
            .addAction(disconnectAction)
            .setCategory(Notification.CATEGORY_SERVICE)
            .setShowWhen(false)
            .setColor(0xFF2196F3.toInt())
            .build()
    }
}
