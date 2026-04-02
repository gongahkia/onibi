import Foundation
import OnibiCore

typealias SessionInputHandler = @Sendable (RemoteInputPayload) async throws -> Void
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

    init(
        id: String,
        displayName: String? = nil,
        startedAt: Date = Date(),
        status: SessionTransportStatus = .starting,
        isControllable: Bool = true,
        workingDirectory: String? = nil,
        lastCommandPreview: String? = nil
    ) {
        self.id = id
        self.displayName = displayName ?? ControllableSessionRegistration.fallbackDisplayName(for: id)
        self.startedAt = startedAt
        self.status = status
        self.isControllable = isControllable
        self.workingDirectory = workingDirectory
        self.lastCommandPreview = lastCommandPreview
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
}

actor ControllableSessionRegistry {
    static let shared = ControllableSessionRegistry()

    private struct SessionRecord {
        var snapshot: ControllableSessionSnapshot
        var buffer: SessionOutputBuffer
        var inputHandler: SessionInputHandler?
        var lastHeartbeatAt: Date
    }

    private var sessions: [String: SessionRecord] = [:]
    private var defaultBufferLineLimit: Int
    private var defaultBufferByteLimit: Int
    private let staleSessionGracePeriod: TimeInterval
    private var proxyRegistrationFailureCount = 0
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
        inputHandler: SessionInputHandler? = nil
    ) {
        let wasExistingSession = sessions[registration.id] != nil
        let existingBuffer = sessions[registration.id]?.buffer ??
            SessionOutputBuffer(
                lineLimit: defaultBufferLineLimit,
                byteLimit: defaultBufferByteLimit
            )
        let currentCursor = existingBuffer.currentCursor
        let currentHandler = inputHandler ?? sessions[registration.id]?.inputHandler

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
                bufferCursor: currentCursor
            ),
            buffer: existingBuffer,
            inputHandler: currentHandler,
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
            bufferCursor: record.buffer.currentCursor
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

        let chunk = record.buffer.append(
            sessionId: sessionId,
            stream: stream,
            data: data,
            timestamp: timestamp
        )
        record.lastHeartbeatAt = timestamp
        record.snapshot = record.snapshot.updating(
            lastActivityAt: timestamp,
            status: .running,
            bufferCursor: record.buffer.currentCursor
        )
        sessions[sessionId] = record
        emit(.output(chunk))
        emit(.sessionUpdated(record.snapshot))
    }

    func bufferSnapshot(for sessionId: String) -> SessionOutputBufferSnapshot? {
        guard let record = sessions[sessionId] else {
            return nil
        }
        return record.buffer.snapshot(for: record.snapshot)
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

        try await inputHandler(payload)
        record.lastHeartbeatAt = timestamp
        record.snapshot = record.snapshot.updating(
            lastActivityAt: timestamp,
            bufferCursor: record.buffer.currentCursor
        )
        sessions[sessionId] = record
        return RemoteInputAcceptance(sessionId: sessionId, acceptedAt: timestamp)
    }

    func markProxyRegistrationFailure() {
        proxyRegistrationFailureCount += 1
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
            proxyRegistrationFailureCount: proxyRegistrationFailureCount
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
        bufferCursor: String? = nil
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
            bufferCursor: bufferCursor ?? self.bufferCursor
        )
    }
}
