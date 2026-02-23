package com.example.natproxy

import android.util.Log
import golib.Golib
import golib.ProtectFunc

/**
 * Build the .aar: cd golib ANDROID_HOME=~/Android/Sdk ANDROID_NDK_HOME=~/Android/Sdk/ndk/<version>
 * \
 * ```
 *     gomobile bind -v -target=android/arm64,android/arm -androidapi=24 \
 *     -o ../android/app/libs/golib.aar ./
 * ```
 */
class GoBridge {
    companion object {
        private const val TAG = "GoBridge"

        fun startServer(settingsJson: String): String {
            Log.d(TAG, "startServer called with settings")
            return Golib.startServer(settingsJson)
        }

        fun startClient(
                code: String,
                tunFd: Int,
                settingsJson: String,
                protectSocket: (Int) -> Boolean
        ) {
            Log.d(TAG, "startClient called with tunFd=$tunFd, settings")
            Golib.startClient(
                    code,
                    tunFd.toLong(),
                    object : ProtectFunc {
                        override fun protect(fd: Long): Boolean {
                            return protectSocket(fd.toInt())
                        }
                    },
                    settingsJson
            )
        }

        fun stopAll() {
            Log.d(TAG, "stopAll called")
            var serverError: Exception? = null
            var clientError: Exception? = null
            try {
                Golib.stopServer()
            } catch (e: Exception) {
                Log.e(TAG, "stopServer failed: ${e.message}", e)
                serverError = e
            }
            try {
                Golib.stopClient()
            } catch (e: Exception) {
                Log.e(TAG, "stopClient failed: ${e.message}", e)
                clientError = e
            }
            // Only throw if both failed — single failures are expected
            if (serverError != null && clientError != null) {
                throw RuntimeException(
                        "stopAll: server=${serverError.message}, client=${clientError.message}",
                        serverError
                )
            }
        }

        fun getServerStatus(): String {
            return Golib.getServerStatus()
        }

        fun getClientStatus(): String {
            return Golib.getClientStatus()
        }

        fun detectNatType(): String {
            return Golib.detectNATType()
        }

        fun getPublicIP(): String {
            return Golib.getPublicIP()
        }

        fun getLogs(cursor: Int): String {
            return Golib.getLogs(cursor.toLong())
        }

        fun clearLogs() {
            Golib.clearLogs()
        }

        fun testLatency(): String {
            return Golib.testLatency()
        }

        fun registerDiscovery(code: String, settingsJson: String): String {
            Log.d(TAG, "registerDiscovery called")
            return Golib.registerServerDiscovery(code, settingsJson)
        }

        fun unregisterDiscovery() {
            Log.d(TAG, "unregisterDiscovery called")
            Golib.unregisterServerDiscovery()
        }

        fun listServers(discoveryUrl: String, room: String): String {
            Log.d(TAG, "listServers called")
            return Golib.listAvailableServers(discoveryUrl, room)
        }

        fun connectWebRTC(code: String, settingsJson: String) {
            Log.d(TAG, "connectWebRTC called")
            Golib.connectWebRTC(code, settingsJson)
        }

        fun startTunnel(
                tunFd: Int,
                settingsJson: String,
                protectSocket: (Int) -> Boolean
        ) {
            Log.d(TAG, "startTunnel called with tunFd=$tunFd")
            Golib.startTunnel(
                    tunFd.toLong(),
                    object : ProtectFunc {
                        override fun protect(fd: Long): Boolean {
                            return protectSocket(fd.toInt())
                        }
                    },
                    settingsJson
            )
        }

        fun waitClientDisconnect(): String {
            return Golib.waitClientDisconnect()
        }

        fun onNetworkChanged(networkType: String) {
            Log.d(TAG, "onNetworkChanged: $networkType")
            Golib.onNetworkChanged(networkType)
        }

        // Manual signaling

        fun startServerManual(settingsJson: String): String {
            Log.d(TAG, "startServerManual called")
            return Golib.startServerManual(settingsJson)
        }

        fun acceptManualAnswer(answerCode: String) {
            Log.d(TAG, "acceptManualAnswer called")
            Golib.acceptManualAnswer(answerCode)
        }

        fun getManualOfferCode(): String {
            return Golib.getManualOfferCode()
        }

        fun processManualOffer(offerCode: String, settingsJson: String): String {
            Log.d(TAG, "processManualOffer called")
            return Golib.processManualOffer(offerCode, settingsJson)
        }

        fun waitManualConnection(timeoutSec: Int) {
            Log.d(TAG, "waitManualConnection called with timeout=$timeoutSec")
            Golib.waitManualConnection(timeoutSec.toLong())
        }
    }
}
