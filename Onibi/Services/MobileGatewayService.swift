import Foundation
import AppKit
import Combine
import Network
import OnibiCore

final class MobileGatewayService: ObservableObject {
    static let shared = MobileGatewayService()

    @Published private(set) var isRunning = false
    @Published private(set) var localURLString = "http://127.0.0.1:8787"
    @Published private(set) var pairingToken = ""
    @Published private(set) var tailscaleStatus: TailscaleServeStatus = .unavailable
    @Published private(set) var lastError: String?

    private let queue = DispatchQueue(label: "com.onibi.mobile-gateway", qos: .userInitiated)
    private let tokenStore = PairingTokenStore(service: "com.onibi.mobile.host", account: "pairing-token")
    private let tailscaleService = TailscaleServeService.shared
    private let dataProvider = HostMobileGatewayDataProvider()
    private let sessionRegistry = ControllableSessionRegistry.shared
    private let realtimeGateway = RealtimeGatewayService.shared
    private let webAssetServer = WebAssetServer()
    private var router: MobileGatewayRouter?
    private var listener: NWListener?
    private var settings: AppSettings
    private var cancellables = Set<AnyCancellable>()

    private init() {
        self.settings = SettingsViewModel.shared.settings
        self.localURLString = "http://127.0.0.1:\(settings.mobileAccessPort)"
        setupSubscriptions()
        loadToken()
        syncRegistrySettings()
    }

    func bootstrap() {
        loadToken()
        localURLString = "http://127.0.0.1:\(settings.mobileAccessPort)"
        syncRegistrySettings()
        if settings.mobileAccessEnabled {
            start()
        } else {
            Task {
                await refreshTailscaleStatus()
            }
        }
    }

    func start() {
        guard listener == nil else {
            DiagnosticsStore.shared.record(
                component: "MobileGatewayService",
                level: .debug,
                message: "start ignored because listener already running"
            )
            return
        }

        loadToken()
        localURLString = "http://127.0.0.1:\(settings.mobileAccessPort)"
        syncRegistrySettings()

        let parameters = NWParameters.tcp
        parameters.allowLocalEndpointReuse = true
        parameters.requiredLocalEndpoint = .hostPort(
            host: "127.0.0.1",
            port: NWEndpoint.Port(rawValue: UInt16(settings.mobileAccessPort)) ?? 8787
        )

        do {
            let listener = try NWListener(using: parameters)
            let router = MobileGatewayRouter(
                tokenProvider: { [tokenStore] in try tokenStore.ensureToken() },
                dataProvider: dataProvider
            )

            Task {
                await realtimeGateway.start()
            }

            listener.stateUpdateHandler = { [weak self] state in
                switch state {
                case .ready:
                    DispatchQueue.main.async {
                        self?.isRunning = true
                        self?.lastError = nil
                    }
                    DiagnosticsStore.shared.record(
                        component: "MobileGatewayService",
                        level: .info,
                        message: "gateway listener is ready",
                        metadata: [
                            "port": String(self?.settings.mobileAccessPort ?? 0)
                        ]
                    )
                case .failed(let error):
                    DispatchQueue.main.async {
                        self?.isRunning = false
                        self?.lastError = error.localizedDescription
                    }
                    DiagnosticsStore.shared.record(
                        component: "MobileGatewayService",
                        level: .error,
                        message: "gateway listener failed",
                        metadata: [
                            "reason": error.localizedDescription
                        ]
                    )
                    ErrorReporter.shared.report(
                        error,
                        context: "MobileGatewayService.listener.stateUpdateHandler",
                        severity: .warning
                    )
                    self?.listener?.cancel()
                    self?.listener = nil
                    Task { [weak self] in
                        guard let self else {
                            return
                        }
                        await self.realtimeGateway.stop()
                    }
                case .cancelled:
                    DispatchQueue.main.async {
                        self?.isRunning = false
                    }
                    DiagnosticsStore.shared.record(
                        component: "MobileGatewayService",
                        level: .info,
                        message: "gateway listener cancelled"
                    )
                    Task { [weak self] in
                        guard let self else {
                            return
                        }
                        await self.realtimeGateway.stop()
                    }
                default:
                    break
                }
            }

            listener.newConnectionHandler = { [weak self] connection in
                self?.handle(connection: connection)
            }

            self.router = router
            self.listener = listener
            listener.start(queue: queue)

            Task {
                await refreshTailscaleStatus()
            }
        } catch {
            DiagnosticsStore.shared.record(
                component: "MobileGatewayService",
                level: .critical,
                message: "failed to start gateway listener",
                metadata: [
                    "reason": error.localizedDescription,
                    "port": String(settings.mobileAccessPort)
                ]
            )
            ErrorReporter.shared.report(error, context: "MobileGatewayService.start", severity: .critical)
            lastError = error.localizedDescription
            isRunning = false
            Task {
                await realtimeGateway.stop()
            }
        }
    }

    func stop() {
        listener?.cancel()
        listener = nil
        router = nil
        isRunning = false
        DiagnosticsStore.shared.record(
            component: "MobileGatewayService",
            level: .info,
            message: "gateway listener stopped"
        )

        Task {
            await realtimeGateway.stop()
            await tailscaleService.disableServe()
            await refreshTailscaleStatus()
        }
    }

    func rotatePairingToken() {
        do {
            pairingToken = try tokenStore.rotateToken()
            lastError = nil
            DiagnosticsStore.shared.record(
                component: "MobileGatewayService",
                level: .info,
                message: "pairing token rotated"
            )
        } catch {
            lastError = error.localizedDescription
            DiagnosticsStore.shared.record(
                component: "MobileGatewayService",
                level: .error,
                message: "pairing token rotation failed",
                metadata: [
                    "reason": error.localizedDescription
                ]
            )
            ErrorReporter.shared.report(error, context: "MobileGatewayService.rotatePairingToken", severity: .warning)
        }
    }

    func copyPairingToken() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(pairingToken, forType: .string)
    }

    func copyBaseURL() {
        if let baseURL = tailscaleStatus.baseURLString {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(baseURL, forType: .string)
        }
    }

    func enableTailscaleServe() {
        Task {
            do {
                let status = try await tailscaleService.enableServe(port: settings.mobileAccessPort)
                await MainActor.run {
                    self.tailscaleStatus = status
                    self.lastError = nil
                }
                DiagnosticsStore.shared.record(
                    component: "MobileGatewayService",
                    level: .info,
                    message: "tailscale serve enabled",
                    metadata: [
                        "port": String(self.settings.mobileAccessPort),
                        "isServing": String(status.isServing)
                    ]
                )
            } catch {
                await MainActor.run {
                    self.lastError = error.localizedDescription
                }
                DiagnosticsStore.shared.record(
                    component: "MobileGatewayService",
                    level: .warning,
                    message: "tailscale serve enable failed",
                    metadata: [
                        "reason": error.localizedDescription
                    ]
                )
                ErrorReporter.shared.report(error, context: "MobileGatewayService.enableTailscaleServe", severity: .warning)
            }
        }
    }

    func refreshTailscaleStatus() async {
        let status = await tailscaleService.refreshStatus(port: settings.mobileAccessPort)
        await MainActor.run {
            self.tailscaleStatus = status
        }
    }

    private func setupSubscriptions() {
        EventBus.shared.settingsPublisher
            .receive(on: DispatchQueue.main)
            .sink { [weak self] newSettings in
                guard let self else { return }
                let previousPort = self.settings.mobileAccessPort
                let previousEnabled = self.settings.mobileAccessEnabled
                self.settings = newSettings
                self.localURLString = "http://127.0.0.1:\(newSettings.mobileAccessPort)"
                self.syncRegistrySettings()

                if newSettings.mobileAccessEnabled {
                    if !previousEnabled || previousPort != newSettings.mobileAccessPort {
                        self.stop()
                        self.start()
                    }
                } else if previousEnabled {
                    self.stop()
                } else {
                    Task {
                        await self.refreshTailscaleStatus()
                    }
                }
            }
            .store(in: &cancellables)
    }

    private func loadToken() {
        do {
            pairingToken = try tokenStore.ensureToken()
        } catch {
            pairingToken = ""
            lastError = error.localizedDescription
            DiagnosticsStore.shared.record(
                component: "MobileGatewayService",
                level: .error,
                message: "failed to load pairing token",
                metadata: [
                    "reason": error.localizedDescription
                ]
            )
            ErrorReporter.shared.report(error, context: "MobileGatewayService.loadToken", severity: .warning)
        }
    }

    private func syncRegistrySettings() {
        let settings = self.settings
        Task {
            await sessionRegistry.configure(
                bufferLineLimit: settings.sessionOutputBufferLineLimit,
                bufferByteLimit: settings.sessionOutputBufferByteLimit
            )
        }
    }

    private func handle(connection: NWConnection) {
        connection.start(queue: queue)
        connection.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) { [weak self] data, _, isComplete, receiveError in
            guard let self else {
                connection.cancel()
                return
            }
            if let receiveError {
                DiagnosticsStore.shared.record(
                    component: "MobileGatewayService",
                    level: .warning,
                    message: "connection receive error",
                    metadata: [
                        "reason": receiveError.localizedDescription
                    ]
                )
            }
            if isComplete && data == nil {
                DiagnosticsStore.shared.record(
                    component: "MobileGatewayService",
                    level: .debug,
                    message: "connection completed without payload"
                )
            }

            guard
                let data,
                let request = self.parseRequest(from: data)
            else {
                DiagnosticsStore.shared.record(
                    component: "MobileGatewayService",
                    level: .warning,
                    message: "rejected malformed gateway request"
                )
                self.send(
                    response: MobileGatewayResponse(
                        statusCode: 400,
                        headers: ["Content-Type": "application/json"],
                        body: Data("{\"error\":\"bad_request\"}".utf8)
                    ),
                    over: connection
                )
                return
            }

            if let webResponse = self.webAssetServer.response(method: request.method, path: request.path) {
                self.send(response: webResponse, over: connection)
                return
            }

            if self.handleRealtimeUpgradeIfNeeded(connection: connection, request: request) {
                return
            }

            guard let router = self.router else {
                self.send(
                    response: MobileGatewayResponse(
                        statusCode: 500,
                        headers: ["Content-Type": "application/json"],
                        body: Data("{\"error\":\"router_unavailable\"}".utf8)
                    ),
                    over: connection
                )
                return
            }

            Task {
                let response = await router.route(
                    method: request.method,
                    path: request.path,
                    queryItems: request.queryItems,
                    headers: request.headers,
                    body: request.body
                )
                self.send(response: response, over: connection)
            }
        }
    }

    private func handleRealtimeUpgradeIfNeeded(
        connection: NWConnection,
        request: ParsedRequest
    ) -> Bool {
        guard GatewayWebSocketConnection.isUpgradeRequest(path: request.path, headers: request.headers) else {
            return false
        }

        guard settings.remoteControlEnabled else {
            send(
                response: MobileGatewayResponse(
                    statusCode: 409,
                    headers: ["Content-Type": "application/json"],
                    body: Data("{\"error\":\"realtime_disabled\"}".utf8)
                ),
                over: connection
            )
            return true
        }

        guard
            let secWebSocketKey = request.headers.first(where: {
                $0.key.caseInsensitiveCompare("Sec-WebSocket-Key") == .orderedSame
            })?.value,
            !secWebSocketKey.isEmpty
        else {
            send(
                response: MobileGatewayResponse(
                    statusCode: 400,
                    headers: ["Content-Type": "application/json"],
                    body: Data("{\"error\":\"invalid_websocket_upgrade\"}".utf8)
                ),
                over: connection
            )
            return true
        }

        let clientID = UUID()
        let realtimeConnection = GatewayWebSocketConnection(
            id: clientID,
            connection: connection,
            queue: queue,
            onText: { [weak self] text in
                guard let self else {
                    return
                }
                Task {
                    await self.realtimeGateway.receive(text: text, from: clientID)
                }
            },
            onDisconnect: { [weak self] disconnectedClientID in
                guard let self else {
                    return
                }
                Task {
                    await self.realtimeGateway.disconnect(clientID: disconnectedClientID)
                }
            }
        )

        connection.send(
            content: GatewayWebSocketConnection.handshakeResponse(for: secWebSocketKey),
            completion: .contentProcessed { [weak self] error in
                guard let self else {
                    connection.cancel()
                    return
                }

                if let error {
                    DiagnosticsStore.shared.record(
                        component: "MobileGatewayService",
                        level: .warning,
                        message: "failed sending websocket upgrade response",
                        metadata: [
                            "reason": error.localizedDescription
                        ]
                    )
                    connection.cancel()
                    return
                }

                Task {
                    await self.realtimeGateway.attach(realtimeConnection)
                    realtimeConnection.start()
                }
            }
        )

        return true
    }

    private func send(response: MobileGatewayResponse, over connection: NWConnection) {
        let reasonPhrase = httpReasonPhrase(for: response.statusCode)
        var responseString = "HTTP/1.1 \(response.statusCode) \(reasonPhrase)\r\n"
        let baseHeaders = response.headers.merging(["Connection": "close"]) { existing, _ in existing }

        for (key, value) in baseHeaders {
            responseString += "\(key): \(value)\r\n"
        }
        responseString += "\r\n"

        var payload = Data(responseString.utf8)
        payload.append(response.body)

        connection.send(content: payload, completion: .contentProcessed { sendError in
            if let sendError {
                DiagnosticsStore.shared.record(
                    component: "MobileGatewayService",
                    level: .warning,
                    message: "failed sending gateway response",
                    metadata: [
                        "reason": sendError.localizedDescription,
                        "statusCode": String(response.statusCode)
                    ]
                )
            }
            connection.cancel()
        })
    }

    private func parseRequest(from data: Data) -> ParsedRequest? {
        guard let requestString = String(data: data, encoding: .utf8) else {
            return nil
        }

        let requestParts = requestString.components(separatedBy: "\r\n\r\n")
        let headerBlock = requestParts.first ?? requestString
        let lines = headerBlock.components(separatedBy: "\r\n")
        guard let requestLine = lines.first else { return nil }

        let requestLineParts = requestLine.split(separator: " ")
        guard requestLineParts.count >= 2 else { return nil }

        let method = String(requestLineParts[0])
        let target = String(requestLineParts[1])
        let url = URL(string: "http://localhost\(target)")
        let path = url?.path ?? target
        let queryItems = URLComponents(url: url ?? URL(string: "http://localhost")!, resolvingAgainstBaseURL: false)?.queryItems ?? []
        let bodyString = requestParts.count > 1 ? requestParts.dropFirst().joined(separator: "\r\n\r\n") : ""

        var headers: [String: String] = [:]
        for line in lines.dropFirst() {
            let components = line.split(separator: ":", maxSplits: 1)
            guard components.count == 2 else { continue }
            headers[String(components[0]).trimmingCharacters(in: .whitespaces)] =
                String(components[1]).trimmingCharacters(in: .whitespaces)
        }

        return ParsedRequest(
            method: method,
            path: path,
            queryItems: queryItems,
            headers: headers,
            body: Data(bodyString.utf8)
        )
    }

    private func httpReasonPhrase(for statusCode: Int) -> String {
        switch statusCode {
        case 200: return "OK"
        case 400: return "Bad Request"
        case 401: return "Unauthorized"
        case 409: return "Conflict"
        case 404: return "Not Found"
        case 405: return "Method Not Allowed"
        case 503: return "Service Unavailable"
        case 500: return "Internal Server Error"
        default: return "OK"
        }
    }
}

private struct ParsedRequest {
    let method: String
    let path: String
    let queryItems: [URLQueryItem]
    let headers: [String: String]
    let body: Data
}

private actor HostMobileGatewayDataProvider: MobileGatewayDataProvider {
    private let storageManager = JSONStorageManager.shared
    private let sessionRegistry = ControllableSessionRegistry.shared

    func health() async throws -> HostHealth {
        let schedulerState = await MainActor.run { BackgroundTaskScheduler.shared.isRunning }
        let lastIngest = await MainActor.run { BackgroundTaskScheduler.shared.lastParseTime }
        let activeSessionCount = await MainActor.run { SessionManager.shared.activeSessionIds.count }
        let ghosttyRunning = await MainActor.run { GhosttyIPCClient.shared.isGhosttyRunning }
        let gatewayRunning = await MainActor.run { MobileGatewayService.shared.isRunning }

        return HostHealth(
            ghosttyRunning: ghosttyRunning,
            schedulerRunning: schedulerState,
            lastIngestAt: lastIngest,
            activeSessionCount: activeSessionCount,
            gatewayRunning: gatewayRunning
        )
    }

    func summary() async throws -> SummaryResponse {
        let logs = try await storageManager.loadLogs()
        let activeSessionCount = await MainActor.run { SessionManager.shared.activeSessionIds.count }
        let recentActivityCount = logs.filter {
            $0.sortTimestamp > Date().addingTimeInterval(-3600)
        }.count
        let assistantBreakdown = logs.reduce(into: [String: Int]()) { result, log in
            result[log.assistantKind.rawValue, default: 0] += 1
        }
        let successfulCommandCount = logs.filter { $0.exitCode == 0 }.count
        let failedCommandCount = logs.filter {
            guard let exitCode = $0.exitCode else { return false }
            return exitCode != 0
        }.count

        return SummaryResponse(
            activeSessionCount: activeSessionCount,
            recentActivityCount: recentActivityCount,
            latestEventAt: logs.map(\.sortTimestamp).max(),
            assistantBreakdown: assistantBreakdown,
            successfulCommandCount: successfulCommandCount,
            failedCommandCount: failedCommandCount
        )
    }

    func sessions() async throws -> [SessionSnapshot] {
        let logs = try await storageManager.loadLogs()
        return await buildSessionSnapshots(from: logs)
    }

    func session(id: String) async throws -> SessionDetail? {
        let logs = try await storageManager.loadLogs()
        let snapshots = await buildSessionSnapshots(from: logs)
        guard let snapshot = snapshots.first(where: { $0.id == id }) else {
            return nil
        }

        let commands = logs
            .filter { $0.sessionId == id }
            .sorted { $0.sortTimestamp > $1.sortTimestamp }
            .map {
                CommandRecordPreview(
                    id: $0.id,
                    sessionId: $0.sessionId ?? id,
                    startedAt: $0.startedAt,
                    endedAt: $0.endedAt,
                    duration: $0.duration,
                    exitCode: $0.exitCode,
                    assistantKind: $0.assistantKind,
                    displayCommand: $0.displayCommand
                )
            }

        return SessionDetail(session: snapshot, commands: commands)
    }

    func events(after cursor: Date?, limit: Int) async throws -> [EventPreview] {
        let logs = try await storageManager.loadLogs()
        return logs
            .sorted { $0.sortTimestamp > $1.sortTimestamp }
            .filter { log in
                guard let cursor else { return true }
                return log.sortTimestamp > cursor
            }
            .prefix(limit)
            .map { log in
                EventPreview(
                    id: log.id,
                    timestamp: log.sortTimestamp,
                    sessionId: log.sessionId ?? "unknown",
                    assistantKind: log.assistantKind,
                    kind: log.assistantKind == .unknown ? .commandCompleted : .assistantActivity,
                    title: log.assistantKind == .unknown ? "Command Completed" : "\(log.assistantKind.displayName) Activity",
                    message: log.displayCommand,
                    exitCode: log.exitCode
                )
            }
    }

    func featureFlags() async throws -> FeatureFlagsResponse {
        let settings = await MainActor.run { SettingsViewModel.shared.settings }
        return FeatureFlagsResponse(
            legacyMonitoringEnabled: true,
            remoteControlEnabled: settings.remoteControlEnabled,
            realtimeSessionsEnabled: settings.remoteControlEnabled,
            websocketEnabled: settings.remoteControlEnabled,
            fallbackInputEnabled: settings.remoteControlEnabled
        )
    }

    func controllableSessions() async throws -> [ControllableSessionSnapshot] {
        await sessionRegistry.sessionsSnapshot()
    }

    func sessionOutputBuffer(id: String) async throws -> SessionOutputBufferSnapshot? {
        await sessionRegistry.bufferSnapshot(for: id)
    }

    func sendInput(
        to sessionId: String,
        payload: RemoteInputPayload
    ) async throws -> RemoteInputAcceptance? {
        try await sessionRegistry.sendInput(payload, to: sessionId)
    }

    func diagnostics() async throws -> DiagnosticsResponse {
        let logs = try await storageManager.loadLogs()
        let storageBytes: Int64
        do {
            storageBytes = try await storageManager.getStorageSize()
        } catch {
            DiagnosticsStore.shared.record(
                component: "HostMobileGatewayDataProvider",
                level: .warning,
                message: "failed to retrieve storage size for diagnostics",
                metadata: [
                    "reason": error.localizedDescription
                ]
            )
            storageBytes = 0
        }

        let schedulerEventsProcessed = await MainActor.run { BackgroundTaskScheduler.shared.eventsProcessed }
        let tailscaleStatus = await MainActor.run { MobileGatewayService.shared.tailscaleStatus }
        let latestError = await MainActor.run { ErrorReporter.shared.recentErrors.first }
        let settings = await MainActor.run { SettingsViewModel.shared.settings }
        let registryDiagnostics = await sessionRegistry.diagnostics()
        let connectedRealtimeClientCount = await RealtimeGatewayService.shared.connectedClientCount()
        let recentDiagnostics = DiagnosticsStore.shared.recentEvents(limit: 25)
            .map {
                DiagnosticsEventPreview(
                    timestamp: $0.timestamp,
                    component: $0.component,
                    severity: mapSeverity($0.level),
                    message: $0.message
                )
            }

        return DiagnosticsResponse(
            generatedAt: Date(),
            hostVersion: Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "unknown",
            diagnosticsEventCount: DiagnosticsStore.shared.totalEventCount(),
            warningCount: DiagnosticsStore.shared.count(for: .warning),
            errorCount: DiagnosticsStore.shared.count(for: .error),
            criticalCount: DiagnosticsStore.shared.count(for: .critical),
            schedulerEventsProcessed: schedulerEventsProcessed,
            storageLogCount: logs.count,
            storageBytes: storageBytes,
            tailscaleStatus: tailscaleStatus.isServing ? "serving" : "not_serving",
            latestErrorTitle: latestError?.title,
            latestErrorTimestamp: latestError?.timestamp,
            recentEvents: recentDiagnostics,
            controllableSessionCount: registryDiagnostics.sessionCount,
            connectedRealtimeClientCount: connectedRealtimeClientCount,
            proxyRegistrationFailureCount: registryDiagnostics.proxyRegistrationFailureCount,
            staleSessionCount: registryDiagnostics.staleSessionCount,
            localProxySocketHealthy: FileManager.default.fileExists(atPath: settings.sessionProxySocketPath)
        )
    }

    private func buildSessionSnapshots(from logs: [LogEntry]) async -> [SessionSnapshot] {
        struct LocalSessionInfo: Sendable {
            let id: String
            let displayName: String
            let isActive: Bool
            let startedAt: Date
            let lastActivityAt: Date
            let commandCount: Int
            let primaryAssistant: AssistantKind
            let lastCommandPreview: String?
        }

        let currentSessions = await MainActor.run {
            (Array(SessionManager.shared.activeSessions.values) + SessionManager.shared.recentSessions).map { session in
                LocalSessionInfo(
                    id: session.id,
                    displayName: SessionManager.shared.displayName(for: session.id),
                    isActive: session.isActive,
                    startedAt: session.startTime,
                    lastActivityAt: session.lastActivityTime,
                    commandCount: session.commandCount,
                    primaryAssistant: session.primaryAssistant,
                    lastCommandPreview: session.lastCommandPreview
                )
            }
        }

        var infoBySession: [String: LocalSessionInfo] = [:]

        for session in currentSessions {
            infoBySession[session.id] = session
        }

        let groupedLogs = Dictionary(grouping: logs) { $0.sessionId ?? "unknown" }
        for (sessionId, sessionLogs) in groupedLogs {
            let sorted = sessionLogs.sorted { $0.sortTimestamp > $1.sortTimestamp }
            let startedAt = sessionLogs.map(\.startedAt).min() ?? Date()
            let lastActivityAt = sorted.first?.sortTimestamp ?? startedAt
            let primaryAssistant = sorted.first(where: { $0.assistantKind != .unknown })?.assistantKind ?? .unknown
            let fallbackName = sessionId.count > 8 ? String(sessionId.prefix(8)) + "..." : sessionId

            let merged = LocalSessionInfo(
                id: sessionId,
                displayName: infoBySession[sessionId]?.displayName ?? fallbackName,
                isActive: infoBySession[sessionId]?.isActive ?? false,
                startedAt: infoBySession[sessionId]?.startedAt ?? startedAt,
                lastActivityAt: infoBySession[sessionId]?.lastActivityAt ?? lastActivityAt,
                commandCount: max(infoBySession[sessionId]?.commandCount ?? 0, sessionLogs.count),
                primaryAssistant: infoBySession[sessionId]?.primaryAssistant == .unknown ? primaryAssistant : infoBySession[sessionId]?.primaryAssistant ?? primaryAssistant,
                lastCommandPreview: infoBySession[sessionId]?.lastCommandPreview ?? sorted.first?.displayCommand
            )
            infoBySession[sessionId] = merged
        }

        return infoBySession.values
            .map {
                SessionSnapshot(
                    id: $0.id,
                    displayName: $0.displayName,
                    isActive: $0.isActive,
                    startedAt: $0.startedAt,
                    lastActivityAt: $0.lastActivityAt,
                    commandCount: $0.commandCount,
                    primaryAssistant: $0.primaryAssistant,
                    lastCommandPreview: $0.lastCommandPreview
                )
            }
            .sorted { $0.lastActivityAt > $1.lastActivityAt }
    }

    private func mapSeverity(_ level: DiagnosticsLevel) -> DiagnosticsSeverity {
        switch level {
        case .debug:
            return .debug
        case .info:
            return .info
        case .warning:
            return .warning
        case .error:
            return .error
        case .critical:
            return .critical
        }
    }
}
