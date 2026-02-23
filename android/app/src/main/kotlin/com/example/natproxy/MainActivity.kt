package com.example.natproxy

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.net.VpnService
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.PowerManager
import android.provider.Settings
import android.util.Log
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.Callable
import java.util.concurrent.ExecutionException
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.TimeoutException

class MainActivity : FlutterActivity() {
    companion object {
        private const val TAG = "MainActivity"
        private const val CHANNEL = "com.p2pshare/vpn"
        private const val STATUS_CHANNEL = "com.p2pshare/status"
        private const val VPN_REQUEST_CODE = 1001
        private const val NOTIFICATION_PERMISSION_CODE = 1002
        private const val BATTERY_OPT_REQUEST_CODE = 1003

        // Timeouts for Go bridge calls (seconds)
        private const val TIMEOUT_START = 60L
        private const val TIMEOUT_STOP = 30L
        private const val TIMEOUT_QUERY = 10L
        private const val TIMEOUT_NAT = 30L
        private const val TIMEOUT_LATENCY = 30L
        private const val TIMEOUT_DISCOVERY = 15L

        @Volatile var statusEventSink: EventChannel.EventSink? = null

        private val mainHandler = Handler(Looper.getMainLooper())

        // Posts a "stopped" event to Flutter, always from the main thread.
        fun sendStopEvent(source: String) {
            mainHandler.post {
                try {
                    statusEventSink?.success(hashMapOf("event" to "stopped", "source" to source))
                } catch (e: Exception) {
                    Log.w(TAG, "sendStopEvent: sink unavailable", e)
                }
            }
        }
    }

    private var pendingResult: MethodChannel.Result? = null
    private var pendingBatteryResult: MethodChannel.Result? = null
    private val stopping = AtomicBoolean(false)
    private val goExecutor: ExecutorService = Executors.newFixedThreadPool(4)
    private val mainHandler = Handler(Looper.getMainLooper())

    // Runs a Go call on the thread pool with a timeout, then posts the result back to main.
    private fun runGoCall(
            timeoutSeconds: Long,
            result: MethodChannel.Result,
            errorCode: String,
            block: () -> Any?
    ) {
        val future = goExecutor.submit(Callable { block() })
        goExecutor.execute {
            try {
                val value = future.get(timeoutSeconds, TimeUnit.SECONDS)
                mainHandler.post { result.success(value) }
            } catch (e: TimeoutException) {
                future.cancel(true)
                Log.e(TAG, "$errorCode timed out after ${timeoutSeconds}s")
                mainHandler.post {
                    result.error(errorCode, "Operation timed out after ${timeoutSeconds}s", null)
                }
            } catch (e: ExecutionException) {
                val cause = e.cause ?: e
                Log.e(TAG, "$errorCode failed", cause)
                mainHandler.post { result.error(errorCode, cause.message, null) }
            } catch (e: Exception) {
                Log.e(TAG, "$errorCode failed", e)
                mainHandler.post { result.error(errorCode, e.message, null) }
            }
        }
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)

        // Request notification permission early to avoid interrupting server/client start
        ensureNotificationPermission()

        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, CHANNEL).setMethodCallHandler {
                call,
                result ->
            when (call.method) {
                "requestVpnPermission" -> requestVpnPermission(result)
                "startServer" -> {
                    val settings = call.argument<String>("settings") ?: "{}"
                    startServer(settings, result)
                }
                "startClient" -> {
                    val code = call.argument<String>("code") ?: ""
                    val settings = call.argument<String>("settings") ?: "{}"
                    startClient(code, settings, result)
                }
                "connectWebRTC" -> {
                    val code = call.argument<String>("code") ?: ""
                    val settings = call.argument<String>("settings") ?: "{}"
                    connectWebRTC(code, settings, result)
                }
                "startVpn" -> {
                    val settings = call.argument<String>("settings") ?: "{}"
                    startVpn(settings, result)
                }
                "stop" -> stop(result)
                "getServerStatus" -> getServerStatus(result)
                "getClientStatus" -> getClientStatus(result)
                "detectNatType" -> detectNatType(result)
                "getLogs" -> getLogs(call.argument<Int>("cursor") ?: 0, result)
                "clearLogs" -> clearLogs(result)
                "testLatency" -> testLatency(result)
                "registerDiscovery" -> {
                    val code = call.argument<String>("code") ?: ""
                    val settings = call.argument<String>("settings") ?: "{}"
                    registerDiscovery(code, settings, result)
                }
                "unregisterDiscovery" -> unregisterDiscovery(result)
                "listServers" -> {
                    val discoveryUrl = call.argument<String>("discoveryUrl")
                        ?: call.argument<String>("signalingUrl") ?: ""
                    val room = call.argument<String>("room") ?: ""
                    listServers(discoveryUrl, room, result)
                }
                "requestBatteryOptimization" -> requestBatteryOptimization(result)
                "startServerManual" -> {
                    val settings = call.argument<String>("settings") ?: "{}"
                    startServerManual(settings, result)
                }
                "acceptManualAnswer" -> {
                    val answerCode = call.argument<String>("answerCode") ?: ""
                    acceptManualAnswer(answerCode, result)
                }
                "processManualOffer" -> {
                    val offerCode = call.argument<String>("offerCode") ?: ""
                    val settings = call.argument<String>("settings") ?: "{}"
                    processManualOffer(offerCode, settings, result)
                }
                "waitManualConnection" -> {
                    val timeout = call.argument<Int>("timeout") ?: 120
                    waitManualConnection(timeout, result)
                }
                else -> result.notImplemented()
            }
        }

        EventChannel(flutterEngine.dartExecutor.binaryMessenger, STATUS_CHANNEL)
                .setStreamHandler(
                        object : EventChannel.StreamHandler {
                            override fun onListen(
                                    arguments: Any?,
                                    events: EventChannel.EventSink?
                            ) {
                                statusEventSink = events
                            }

                            override fun onCancel(arguments: Any?) {
                                statusEventSink = null
                            }
                        }
                )
    }

    override fun onDestroy() {
        // Resolve pending results to avoid Flutter hang
        pendingResult?.success(false)
        pendingResult = null
        pendingBatteryResult?.success(false)
        pendingBatteryResult = null
        statusEventSink = null
        goExecutor.shutdownNow()
        super.onDestroy()
    }

    private fun requestVpnPermission(result: MethodChannel.Result) {
        val intent = VpnService.prepare(this)
        if (intent != null) {
            pendingResult = result
            startActivityForResult(intent, VPN_REQUEST_CODE)
        } else {
            result.success(true)
        }
    }

    private fun requestBatteryOptimization(result: MethodChannel.Result) {
        val pm = getSystemService(POWER_SERVICE) as PowerManager
        if (pm.isIgnoringBatteryOptimizations(packageName)) {
            result.success(true)
            return
        }
        pendingBatteryResult = result
        val intent = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS).apply {
            data = Uri.parse("package:$packageName")
        }
        startActivityForResult(intent, BATTERY_OPT_REQUEST_CODE)
    }

    private fun ensureNotificationPermission() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            if (ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) !=
                            PackageManager.PERMISSION_GRANTED
            ) {
                ActivityCompat.requestPermissions(
                        this,
                        arrayOf(Manifest.permission.POST_NOTIFICATIONS),
                        NOTIFICATION_PERMISSION_CODE
                )
            }
        }
    }

    private fun startServer(settingsJson: String, result: MethodChannel.Result) {
        val future = goExecutor.submit(Callable { GoBridge.startServer(settingsJson) })
        goExecutor.execute {
            try {
                val connectionCode = future.get(TIMEOUT_START, TimeUnit.SECONDS)
                Log.i(TAG, "Server started, code length=${connectionCode.length}")
                mainHandler.post {
                    val svcIntent = Intent(this, ServerForegroundService::class.java)
                    startForegroundService(svcIntent)
                    result.success(connectionCode)
                }
            } catch (e: TimeoutException) {
                future.cancel(true)
                Log.e(TAG, "startServer timed out after ${TIMEOUT_START}s")
                mainHandler.post {
                    result.error(
                            "SERVER_ERROR",
                            "Operation timed out after ${TIMEOUT_START}s",
                            null
                    )
                }
            } catch (e: ExecutionException) {
                val cause = e.cause ?: e
                Log.e(TAG, "startServer failed", cause)
                mainHandler.post { result.error("SERVER_ERROR", cause.message, null) }
            } catch (e: Exception) {
                Log.e(TAG, "startServer failed", e)
                mainHandler.post { result.error("SERVER_ERROR", e.message, null) }
            }
        }
    }

    private fun startClient(code: String, settingsJson: String, result: MethodChannel.Result) {
        try {
            val vpnIntent = VpnService.prepare(this)
            if (vpnIntent != null) {
                Log.w(TAG, "VPN permission not granted")
                result.error("VPN_PERMISSION", "VPN permission not granted", null)
                return
            }
            val serviceIntent =
                    Intent(this, ProxyVpnService::class.java).apply {
                        putExtra("connection_code", code)
                        putExtra("settings_json", settingsJson)
                    }
            startForegroundService(serviceIntent)
            Log.i(TAG, "Client VPN service started")
            result.success(null)
        } catch (e: Exception) {
            Log.e(TAG, "startClient failed", e)
            result.error("CLIENT_ERROR", e.message, null)
        }
    }

    // Phase 1: connect WebRTC before the TUN is up (blocks until connected or failed).
    private fun connectWebRTC(code: String, settingsJson: String, result: MethodChannel.Result) {
        runGoCall(TIMEOUT_START, result, "CONNECT_ERROR") {
            GoBridge.connectWebRTC(code, settingsJson)
            null
        }
    }

    // Phase 2: WebRTC is up, now bring the TUN interface online.
    private fun startVpn(settingsJson: String, result: MethodChannel.Result) {
        try {
            val vpnIntent = VpnService.prepare(this)
            if (vpnIntent != null) {
                Log.w(TAG, "VPN permission not granted")
                result.error("VPN_PERMISSION", "VPN permission not granted", null)
                return
            }
            val serviceIntent =
                    Intent(this, ProxyVpnService::class.java).apply {
                        putExtra("mode", "tunnel")
                        putExtra("settings_json", settingsJson)
                    }
            startForegroundService(serviceIntent)
            Log.i(TAG, "VPN tunnel service started")
            result.success(null)
        } catch (e: Exception) {
            Log.e(TAG, "startVpn failed", e)
            result.error("VPN_ERROR", e.message, null)
        }
    }

    private fun stop(result: MethodChannel.Result) {
        if (!stopping.compareAndSet(false, true)) {
            result.success(null)
            return
        }

        // Capture instance refs to avoid races with other threads
        val vpn = ProxyVpnService.instance
        val serverSvc = ServerForegroundService.instance

        val future =
                goExecutor.submit(
                        Callable {
                            if (vpn != null) {
                                vpn.shutdown()
                            } else {
                                GoBridge.stopAll()
                            }
                            serverSvc?.shutdown()
                        }
                )
        goExecutor.execute {
            var error: String? = null
            try {
                future.get(TIMEOUT_STOP, TimeUnit.SECONDS)
            } catch (e: TimeoutException) {
                future.cancel(true)
                Log.e(TAG, "stop timed out after ${TIMEOUT_STOP}s")
                error = "Operation timed out after ${TIMEOUT_STOP}s"
            } catch (e: ExecutionException) {
                val cause = e.cause ?: e
                Log.e(TAG, "stop failed", cause)
                error = cause.message
            } catch (e: Exception) {
                Log.e(TAG, "stop failed", e)
                error = e.message
            }
            // Always stop services even if Go cleanup failed
            mainHandler.post {
                try {
                    stopService(Intent(this, ProxyVpnService::class.java))
                    stopService(Intent(this, ServerForegroundService::class.java))
                } catch (e: Exception) {
                    Log.w(TAG, "Error stopping services", e)
                }
                stopping.set(false)
                if (error != null) {
                    Log.w(TAG, "stop completed with error: $error")
                    // Still report success since services are stopped
                    result.success(null)
                } else {
                    Log.i(TAG, "Stopped all services")
                    result.success(null)
                }
            }
        }
    }

    private fun getServerStatus(result: MethodChannel.Result) {
        runGoCall(TIMEOUT_QUERY, result, "STATUS_ERROR") { GoBridge.getServerStatus() }
    }

    private fun getClientStatus(result: MethodChannel.Result) {
        runGoCall(TIMEOUT_QUERY, result, "STATUS_ERROR") { GoBridge.getClientStatus() }
    }

    private fun getLogs(cursor: Int, result: MethodChannel.Result) {
        runGoCall(TIMEOUT_QUERY, result, "LOG_ERROR") { GoBridge.getLogs(cursor) }
    }

    private fun clearLogs(result: MethodChannel.Result) {
        runGoCall(TIMEOUT_QUERY, result, "LOG_ERROR") {
            GoBridge.clearLogs()
            null
        }
    }

    private fun testLatency(result: MethodChannel.Result) {
        runGoCall(TIMEOUT_LATENCY, result, "LATENCY_ERROR") { GoBridge.testLatency() }
    }

    private fun detectNatType(result: MethodChannel.Result) {
        runGoCall(TIMEOUT_NAT, result, "NAT_ERROR") { GoBridge.detectNatType() }
    }

    private fun registerDiscovery(
            code: String,
            settingsJson: String,
            result: MethodChannel.Result
    ) {
        runGoCall(TIMEOUT_DISCOVERY, result, "DISCOVERY_ERROR") {
            GoBridge.registerDiscovery(code, settingsJson)
        }
    }

    private fun unregisterDiscovery(result: MethodChannel.Result) {
        runGoCall(TIMEOUT_DISCOVERY, result, "DISCOVERY_ERROR") {
            GoBridge.unregisterDiscovery()
            null
        }
    }

    private fun listServers(discoveryUrl: String, room: String, result: MethodChannel.Result) {
        runGoCall(TIMEOUT_DISCOVERY, result, "DISCOVERY_ERROR") {
            GoBridge.listServers(discoveryUrl, room)
        }
    }

    // Manual signaling

    private fun startServerManual(settingsJson: String, result: MethodChannel.Result) {
        val future = goExecutor.submit(Callable { GoBridge.startServerManual(settingsJson) })
        goExecutor.execute {
            try {
                val offerCode = future.get(TIMEOUT_START, TimeUnit.SECONDS)
                Log.i(TAG, "Manual server started, offer code length=${offerCode.length}")
                mainHandler.post {
                    val svcIntent = Intent(this, ServerForegroundService::class.java)
                    startForegroundService(svcIntent)
                    result.success(offerCode)
                }
            } catch (e: TimeoutException) {
                future.cancel(true)
                Log.e(TAG, "startServerManual timed out after ${TIMEOUT_START}s")
                mainHandler.post {
                    result.error("SERVER_ERROR", "Operation timed out after ${TIMEOUT_START}s", null)
                }
            } catch (e: ExecutionException) {
                val cause = e.cause ?: e
                Log.e(TAG, "startServerManual failed", cause)
                mainHandler.post { result.error("SERVER_ERROR", cause.message, null) }
            } catch (e: Exception) {
                Log.e(TAG, "startServerManual failed", e)
                mainHandler.post { result.error("SERVER_ERROR", e.message, null) }
            }
        }
    }

    private fun acceptManualAnswer(answerCode: String, result: MethodChannel.Result) {
        runGoCall(120L, result, "MANUAL_ERROR") {
            GoBridge.acceptManualAnswer(answerCode)
            null
        }
    }

    private fun processManualOffer(
            offerCode: String,
            settingsJson: String,
            result: MethodChannel.Result
    ) {
        runGoCall(TIMEOUT_START, result, "MANUAL_ERROR") {
            GoBridge.processManualOffer(offerCode, settingsJson)
        }
    }

    private fun waitManualConnection(timeoutSec: Int, result: MethodChannel.Result) {
        runGoCall(timeoutSec.toLong() + 5, result, "MANUAL_ERROR") {
            GoBridge.waitManualConnection(timeoutSec)
            null
        }
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode == VPN_REQUEST_CODE) {
            pendingResult?.success(resultCode == Activity.RESULT_OK)
            pendingResult = null
        } else if (requestCode == BATTERY_OPT_REQUEST_CODE) {
            val pm = getSystemService(POWER_SERVICE) as PowerManager
            pendingBatteryResult?.success(pm.isIgnoringBatteryOptimizations(packageName))
            pendingBatteryResult = null
        }
    }
}
