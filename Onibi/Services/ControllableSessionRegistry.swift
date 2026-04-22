import Foundation
import OnibiCore

typealias SessionInputHandler = @Sendable (RemoteInputPayload) async throws -> Void
typealias SessionResizeHandler = @Sendable (RemoteTerminalResizePayload) async throws -> Void
typealias ControllableSessionRegistryObserver = @Sendable (ControllableSessionRegistryEvent) -> Void

enum ControllableSessionRegistryEvent: Equatable, Sendable {
    case sessionAdded(ControllableSessionSnapshot)
    case sessionUpdated(ControllableSessionSnapshot)
    case sessionRemoved(String)
    case output(SessionOutputChunk)
}

struct ControllableSessionRegistration: Sendable {
    let id: String
    let displayName: String
    let startedAt: Date
    let status: SessionTransportStatus
    let isControllable: Bool
    let workingDirectory: String?
    let lastCommandPreview: String?
    let shell: String?
    let pid: Int32?
    let hostname: String?
    let proxyVersion: String?
    let terminalCols: Int?
    let terminalRows: Int?
    let terminalTitle: String?
    let lastTerminalEvent: TerminalEventSnapshot?
    let health: SessionHealthSnapshot?

    init(
        id: String,
        displayName: String? = nil,
        startedAt: Date = Date(),
        status: SessionTransportStatus = .starting,
        isControllable: Bool = true,
        workingDirectory: String? = nil,
        lastCommandPreview: String? = nil,
        shell: String? = nil,
        pid: Int32? = nil,
        hostname: String? = nil,
        proxyVersion: String? = nil,
        terminalCols: Int? = nil,
        terminalRows: Int? = nil,
        terminalTitle: String? = nil,
        lastTerminalEvent: TerminalEventSnapshot? = nil,
        health: SessionHealthSnapshot? = nil
    ) {
        self.id = id
        self.displayName = displayName ?? ControllableSessionRegistration.fallbackDisplayName(for: id)
        self.startedAt = startedAt
        self.status = status
        self.isControllable = isControllable
        self.workingDirectory = workingDirectory
        self.lastCommandPreview = lastCommandPreview
        self.shell = shell
        self.pid = pid
        self.hostname = hostname
        self.proxyVersion = proxyVersion
        self.terminalCols = terminalCols
        self.terminalRows = terminalRows
        self.terminalTitle = terminalTitle
        self.lastTerminalEvent = lastTerminalEvent
        self.health = health
    }

    private static func fallbackDisplayName(for sessionId: String) -> String {
        if sessionId.count > 12 {
            return String(sessionId.prefix(12)) + "..."
        }
        return sessionId
    }
}

struct ControllableSessionRegistryDiagnostics: Equatable, Sendable {
    let sessionCount: Int
    let staleSessionCount: Int
    let proxyRegistrationFailureCount: Int
    let proxyDisconnectCount: Int
    let bufferTruncationCount: Int
    let lastInputRoutingError: String?
    let latestProxyVersion: String?
    let proxyVersionMismatchCount: Int
}

actor ControllableSessionRegistry {
    static let shared = ControllableSessionRegistry()

    private struct SessionRecord {
        var snapshot: ControllableSessionSnapshot
        var buffer: SessionOutputBuffer
        var inputHandler: SessionInputHandler?
        var resizeHandler: SessionResizeHandler?
        var lastHeartbeatAt: Date
    }

    private var sessions: [String: SessionRecord] = [:]
    private var defaultBufferLineLimit: Int
    private var defaultBufferByteLimit: Int
    private let staleSessionGracePeriod: TimeInterval
    private var proxyRegistrationFailureCount = 0
    private var proxyDisconnectCount = 0
    private var bufferTruncationCount = 0
    private var lastInputRoutingError: String?
    private var latestProxyVersion: String?
    private var proxyVersionMismatchCount = 0
    private var observers: [UUID: ControllableSessionRegistryObserver] = [:]

    init(
        defaultBufferLineLimit: Int = 1000,
        defaultBufferByteLimit: Int = 256 * 1024,
        staleSessionGracePeriod: TimeInterval = 30
    ) {
        self.defaultBufferLineLimit = max(1, defaultBufferLineLimit)
        self.defaultBufferByteLimit = max(1, defaultBufferByteLimit)
        self.staleSessionGracePeriod = staleSessionGracePeriod
    }

    func configure(bufferLineLimit: Int, bufferByteLimit: Int) {
        defaultBufferLineLimit = max(1, bufferLineLimit)
        defaultBufferByteLimit = max(1, bufferByteLimit)

        for sessionId in sessions.keys {
            guard var record = sessions[sessionId] else {
                continue
            }
            record.buffer.reconfigure(
                lineLimit: defaultBufferLineLimit,
                byteLimit: defaultBufferByteLimit
            )
            record.snapshot = record.snapshot.updating(bufferCursor: record.buffer.currentCursor)
            sessions[sessionId] = record
        }
    }

    func clearAll() {
        let removedSessionIDs = Array(sessions.keys)
        sessions.removeAll()
        proxyRegistrationFailureCount = 0
        proxyDisconnectCount = 0
        bufferTruncationCount = 0
        lastInputRoutingError = nil
        latestProxyVersion = nil
        proxyVersionMismatchCount = 0
        for sessionId in removedSessionIDs {
            emit(.sessionRemoved(sessionId))
        }
    }

    func addObserver(_ observer: @escaping ControllableSessionRegistryObserver) -> UUID {
        let id = UUID()
        observers[id] = observer
        return id
    }

    func removeObserver(id: UUID) {
        observers.removeValue(forKey: id)
    }

    func register(
        _ registration: ControllableSessionRegistration,
        inputHandler: SessionInputHandler? = nil,
        resizeHandler: SessionResizeHandler? = nil
    ) {
        let wasExistingSession = sessions[registration.id] != nil
        let existingBuffer = sessions[registration.id]?.buffer ??
            SessionOutputBuffer(
                lineLimit: defaultBufferLineLimit,
                byteLimit: defaultBufferByteLimit
            )
        let currentCursor = existingBuffer.currentCursor
        let currentHandler = inputHandler ?? sessions[registration.id]?.inputHandler
        let currentResizeHandler = resizeHandler ?? sessions[registration.id]?.resizeHandler

        sessions[registration.id] = SessionRecord(
            snapshot: ControllableSessionSnapshot(
                id: registration.id,
                displayName: registration.displayName,
                startedAt: registration.startedAt,
                lastActivityAt: registration.startedAt,
                status: registration.status,
                isControllable: registration.isControllable,
                workingDirectory: registration.workingDirectory,
                lastCommandPreview: registration.lastCommandPreview,
                bufferCursor: currentCursor,
                shell: registration.shell,
                pid: registration.pid,
                hostname: registration.hostname,
                proxyVersion: registration.proxyVersion,
                terminalCols: registration.terminalCols,
                terminalRows: registration.terminalRows,
                terminalTitle: registration.terminalTitle,
                lastTerminalEvent: registration.lastTerminalEvent,
                health: registration.health
            ),
            buffer: existingBuffer,
            inputHandler: currentHandler,
            resizeHandler: currentResizeHandler,
            lastHeartbeatAt: registration.startedAt
        )

        if let snapshot = sessions[registration.id]?.snapshot {
            emit(wasExistingSession ? .sessionUpdated(snapshot) : .sessionAdded(snapshot))
        }
    }

    func session(id: String) -> ControllableSessionSnapshot? {
        sessions[id]?.snapshot
    }

    func sessionsSnapshot() -> [ControllableSessionSnapshot] {
        sessions.values
            .map(\.snapshot)
            .sorted { $0.lastActivityAt > $1.lastActivityAt }
    }

    func setInputHandler(_ inputHandler: SessionInputHandler?, for sessionId: String) {
        guard var record = sessions[sessionId] else {
            return
        }
        record.inputHandler = inputHandler
        sessions[sessionId] = record
    }

    func setResizeHandler(_ resizeHandler: SessionResizeHandler?, for sessionId: String) {
        guard var record = sessions[sessionId] else {
            return
        }
        record.resizeHandler = resizeHandler
        sessions[sessionId] = record
    }

    func recordHeartbeat(for sessionId: String, at timestamp: Date = Date()) {
        guard var record = sessions[sessionId] else {
            return
        }
        record.lastHeartbeatAt = timestamp
        record.snapshot = record.snapshot.updating(lastActivityAt: timestamp)
        sessions[sessionId] = record
    }

    func updateSession(
        id sessionId: String,
        status: SessionTransportStatus? = nil,
        workingDirectory: String? = nil,
        lastCommandPreview: String? = nil,
        displayName: String? = nil,
        isControllable: Bool? = nil,
        terminalCols: Int? = nil,
        terminalRows: Int? = nil,
        terminalTitle: String? = nil,
        lastTerminalEvent: TerminalEventSnapshot? = nil,
        health: SessionHealthSnapshot? = nil,
        at timestamp: Date = Date()
    ) {
        guard var record = sessions[sessionId] else {
            return
        }

        record.lastHeartbeatAt = timestamp
        record.snapshot = record.snapshot.updating(
            displayName: displayName,
            lastActivityAt: timestamp,
            status: status,
            isControllable: isControllable,
            workingDirectory: workingDirectory ?? record.snapshot.workingDirectory,
            lastCommandPreview: lastCommandPreview ?? record.snapshot.lastCommandPreview,
            bufferCursor: record.buffer.currentCursor,
            terminalCols: terminalCols ?? record.snapshot.terminalCols,
            terminalRows: terminalRows ?? record.snapshot.terminalRows,
            terminalTitle: terminalTitle ?? record.snapshot.terminalTitle,
            lastTerminalEvent: lastTerminalEvent ?? record.snapshot.lastTerminalEvent,
            health: health ?? record.snapshot.health
        )
        sessions[sessionId] = record
        emit(.sessionUpdated(record.snapshot))
    }

    func appendOutput(
        sessionId: String,
        stream: SessionOutputStream = .stdout,
        data: Data,
        timestamp: Date = Date()
    ) {
        guard var record = sessions[sessionId] else {
            return
        }

        let appendResult = record.buffer.append(
            sessionId: sessionId,
            stream: stream,
            data: data,
            timestamp: timestamp
        )
        if appendResult.truncationEventCount > 0 {
            bufferTruncationCount += appendResult.truncationEventCount
        }
        record.lastHeartbeatAt = timestamp
        record.snapshot = record.snapshot.updating(
            lastActivityAt: timestamp,
            status: .running,
            bufferCursor: record.buffer.currentCursor
        )
        sessions[sessionId] = record
        emit(.output(appendResult.chunk))
        emit(.sessionUpdated(record.snapshot))
    }

    func bufferSnapshot(
        for sessionId: String,
        after cursor: String? = nil,
        limit: Int? = nil
    ) -> SessionOutputBufferSnapshot? {
        guard let record = sessions[sessionId] else {
            return nil
        }
        return record.buffer.snapshot(for: record.snapshot, after: cursor, limit: limit)
    }

    func sendInput(
        _ payload: RemoteInputPayload,
        to sessionId: String,
        at timestamp: Date = Date()
    ) async throws -> RemoteInputAcceptance {
        guard payload.isValid else {
            throw RemoteControlError.invalidInputPayload
        }

        guard var record = sessions[sessionId] else {
            throw RemoteControlError.sessionNotFound(sessionId)
        }

        guard record.snapshot.isControllable else {
            throw RemoteControlError.sessionNotControllable(sessionId)
        }

        guard let inputHandler = record.inputHandler else {
            throw RemoteControlError.inputUnavailable(sessionId)
        }

        do {
            try await inputHandler(payload)
        } catch {
            lastInputRoutingError = "send_input[\(sessionId)]: \(error.localizedDescription)"
            throw error
        }
        record.lastHeartbeatAt = timestamp
        record.snapshot = record.snapshot.updating(
            lastActivityAt: timestamp,
            bufferCursor: record.buffer.currentCursor
        )
        sessions[sessionId] = record
        return RemoteInputAcceptance(sessionId: sessionId, acceptedAt: timestamp)
    }

    func resizeTerminal(
        _ payload: RemoteTerminalResizePayload,
        for sessionId: String,
        at timestamp: Date = Date()
    ) async throws -> RemoteTerminalResizeAcceptance {
        guard payload.isValid else {
            throw RemoteControlError.invalidResizePayload
        }

        guard var record = sessions[sessionId] else {
            throw RemoteControlError.sessionNotFound(sessionId)
        }

        guard record.snapshot.isControllable else {
            throw RemoteControlError.sessionNotControllable(sessionId)
        }

        guard let resizeHandler = record.resizeHandler else {
            throw RemoteControlError.resizeUnavailable(sessionId)
        }

        do {
            try await resizeHandler(payload)
        } catch {
            lastInputRoutingError = "resize[\(sessionId)]: \(error.localizedDescription)"
            throw error
        }
        record.lastHeartbeatAt = timestamp
        record.snapshot = record.snapshot.updating(
            lastActivityAt: timestamp,
            bufferCursor: record.buffer.currentCursor,
            terminalCols: payload.cols,
            terminalRows: payload.rows
        )
        sessions[sessionId] = record
        return RemoteTerminalResizeAcceptance(
            sessionId: sessionId,
            cols: payload.cols,
            rows: payload.rows,
            acceptedAt: timestamp
        )
    }

    func markProxyRegistrationFailure() {
        proxyRegistrationFailureCount += 1
    }

    func markProxyDisconnect() {
        proxyDisconnectCount += 1
    }

    func recordProxyVersion(_ version: String, isCompatible: Bool) {
        latestProxyVersion = version
        if !isCompatible {
            proxyVersionMismatchCount += 1
        }
    }

    func expireDisconnectedSessions(now: Date = Date()) {
        let staleSessionIds = sessions.compactMap { sessionId, record -> String? in
            let age = now.timeIntervalSince(record.lastHeartbeatAt)
            if age > staleSessionGracePeriod && record.snapshot.status != .running {
                return sessionId
            }
            return nil
        }

        for sessionId in staleSessionIds {
            sessions.removeValue(forKey: sessionId)
            emit(.sessionRemoved(sessionId))
        }
    }

    func diagnostics(now: Date = Date()) -> ControllableSessionRegistryDiagnostics {
        let staleSessionCount = sessions.values.reduce(into: 0) { count, record in
            if now.timeIntervalSince(record.lastHeartbeatAt) > staleSessionGracePeriod {
                count += 1
            }
        }

        return ControllableSessionRegistryDiagnostics(
            sessionCount: sessions.count,
            staleSessionCount: staleSessionCount,
            proxyRegistrationFailureCount: proxyRegistrationFailureCount,
            proxyDisconnectCount: proxyDisconnectCount,
            bufferTruncationCount: bufferTruncationCount,
            lastInputRoutingError: lastInputRoutingError,
            latestProxyVersion: latestProxyVersion,
            proxyVersionMismatchCount: proxyVersionMismatchCount
        )
    }

    private func emit(_ event: ControllableSessionRegistryEvent) {
        for observer in observers.values {
            observer(event)
        }
    }
}

private extension ControllableSessionSnapshot {
    func updating(
        displayName: String? = nil,
        lastActivityAt: Date? = nil,
        status: SessionTransportStatus? = nil,
        isControllable: Bool? = nil,
        workingDirectory: String? = nil,
        lastCommandPreview: String? = nil,
        bufferCursor: String? = nil,
        shell: String? = nil,
        pid: Int32? = nil,
        hostname: String? = nil,
        proxyVersion: String? = nil,
        terminalCols: Int? = nil,
        terminalRows: Int? = nil,
        terminalTitle: String? = nil,
        lastTerminalEvent: TerminalEventSnapshot? = nil,
        health: SessionHealthSnapshot? = nil
    ) -> ControllableSessionSnapshot {
        ControllableSessionSnapshot(
            id: id,
            displayName: displayName ?? self.displayName,
            startedAt: startedAt,
            lastActivityAt: lastActivityAt ?? self.lastActivityAt,
            status: status ?? self.status,
            isControllable: isControllable ?? self.isControllable,
            workingDirectory: workingDirectory ?? self.workingDirectory,
            lastCommandPreview: lastCommandPreview ?? self.lastCommandPreview,
            bufferCursor: bufferCursor ?? self.bufferCursor,
            shell: shell ?? self.shell,
            pid: pid ?? self.pid,
            hostname: hostname ?? self.hostname,
            proxyVersion: proxyVersion ?? self.proxyVersion,
            terminalCols: terminalCols ?? self.terminalCols,
            terminalRows: terminalRows ?? self.terminalRows,
            terminalTitle: terminalTitle ?? self.terminalTitle,
            lastTerminalEvent: lastTerminalEvent ?? self.lastTerminalEvent,
            health: health ?? self.health
        )
    }
}
