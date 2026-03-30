import Foundation

public enum MobileEventKind: String, Codable, Sendable {
    case commandCompleted
    case assistantActivity
}

public enum DiagnosticsSeverity: String, Codable, Sendable {
    case debug
    case info
    case warning
    case error
    case critical
}

public struct HostHealth: Codable, Equatable, Sendable {
    public let ghosttyRunning: Bool
    public let schedulerRunning: Bool
    public let lastIngestAt: Date?
    public let activeSessionCount: Int
    public let gatewayRunning: Bool

    public init(
        ghosttyRunning: Bool,
        schedulerRunning: Bool,
        lastIngestAt: Date?,
        activeSessionCount: Int,
        gatewayRunning: Bool
    ) {
        self.ghosttyRunning = ghosttyRunning
        self.schedulerRunning = schedulerRunning
        self.lastIngestAt = lastIngestAt
        self.activeSessionCount = activeSessionCount
        self.gatewayRunning = gatewayRunning
    }
}

public struct SummaryResponse: Codable, Equatable, Sendable {
    public let activeSessionCount: Int
    public let recentActivityCount: Int
    public let latestEventAt: Date?

    public init(activeSessionCount: Int, recentActivityCount: Int, latestEventAt: Date?) {
        self.activeSessionCount = activeSessionCount
        self.recentActivityCount = recentActivityCount
        self.latestEventAt = latestEventAt
    }
}

public struct SessionSnapshot: Codable, Equatable, Identifiable, Sendable {
    public let id: String
    public let displayName: String
    public let isActive: Bool
    public let startedAt: Date
    public let lastActivityAt: Date
    public let commandCount: Int
    public let primaryAssistant: AssistantKind
    public let lastCommandPreview: String?

    public init(
        id: String,
        displayName: String,
        isActive: Bool,
        startedAt: Date,
        lastActivityAt: Date,
        commandCount: Int,
        primaryAssistant: AssistantKind,
        lastCommandPreview: String?
    ) {
        self.id = id
        self.displayName = displayName
        self.isActive = isActive
        self.startedAt = startedAt
        self.lastActivityAt = lastActivityAt
        self.commandCount = commandCount
        self.primaryAssistant = primaryAssistant
        self.lastCommandPreview = lastCommandPreview
    }
}

public struct CommandRecordPreview: Codable, Equatable, Identifiable, Sendable {
    public let id: UUID
    public let sessionId: String
    public let startedAt: Date
    public let endedAt: Date?
    public let duration: TimeInterval?
    public let exitCode: Int?
    public let assistantKind: AssistantKind
    public let displayCommand: String

    public init(
        id: UUID,
        sessionId: String,
        startedAt: Date,
        endedAt: Date?,
        duration: TimeInterval?,
        exitCode: Int?,
        assistantKind: AssistantKind,
        displayCommand: String
    ) {
        self.id = id
        self.sessionId = sessionId
        self.startedAt = startedAt
        self.endedAt = endedAt
        self.duration = duration
        self.exitCode = exitCode
        self.assistantKind = assistantKind
        self.displayCommand = displayCommand
    }
}

public struct SessionDetail: Codable, Equatable, Sendable {
    public let session: SessionSnapshot
    public let commands: [CommandRecordPreview]

    public init(session: SessionSnapshot, commands: [CommandRecordPreview]) {
        self.session = session
        self.commands = commands
    }
}

public struct EventPreview: Codable, Equatable, Identifiable, Sendable {
    public let id: UUID
    public let timestamp: Date
    public let sessionId: String
    public let assistantKind: AssistantKind
    public let kind: MobileEventKind
    public let title: String
    public let message: String
    public let exitCode: Int?

    public init(
        id: UUID,
        timestamp: Date,
        sessionId: String,
        assistantKind: AssistantKind,
        kind: MobileEventKind,
        title: String,
        message: String,
        exitCode: Int?
    ) {
        self.id = id
        self.timestamp = timestamp
        self.sessionId = sessionId
        self.assistantKind = assistantKind
        self.kind = kind
        self.title = title
        self.message = message
        self.exitCode = exitCode
    }
}

public struct DiagnosticsEventPreview: Codable, Equatable, Sendable {
    public let timestamp: Date
    public let component: String
    public let severity: DiagnosticsSeverity
    public let message: String

    public init(
        timestamp: Date,
        component: String,
        severity: DiagnosticsSeverity,
        message: String
    ) {
        self.timestamp = timestamp
        self.component = component
        self.severity = severity
        self.message = message
    }
}

public struct DiagnosticsResponse: Codable, Equatable, Sendable {
    public let generatedAt: Date
    public let hostVersion: String
    public let diagnosticsEventCount: Int
    public let warningCount: Int
    public let errorCount: Int
    public let criticalCount: Int
    public let schedulerEventsProcessed: Int
    public let storageLogCount: Int
    public let storageBytes: Int64
    public let tailscaleStatus: String
    public let latestErrorTitle: String?
    public let latestErrorTimestamp: Date?
    public let recentEvents: [DiagnosticsEventPreview]

    public init(
        generatedAt: Date,
        hostVersion: String,
        diagnosticsEventCount: Int,
        warningCount: Int,
        errorCount: Int,
        criticalCount: Int,
        schedulerEventsProcessed: Int,
        storageLogCount: Int,
        storageBytes: Int64,
        tailscaleStatus: String,
        latestErrorTitle: String?,
        latestErrorTimestamp: Date?,
        recentEvents: [DiagnosticsEventPreview]
    ) {
        self.generatedAt = generatedAt
        self.hostVersion = hostVersion
        self.diagnosticsEventCount = diagnosticsEventCount
        self.warningCount = warningCount
        self.errorCount = errorCount
        self.criticalCount = criticalCount
        self.schedulerEventsProcessed = schedulerEventsProcessed
        self.storageLogCount = storageLogCount
        self.storageBytes = storageBytes
        self.tailscaleStatus = tailscaleStatus
        self.latestErrorTitle = latestErrorTitle
        self.latestErrorTimestamp = latestErrorTimestamp
        self.recentEvents = recentEvents
    }
}

public struct MobileConnectionConfiguration: Codable, Equatable, Sendable {
    public let baseURLString: String

    public init(baseURLString: String) {
        self.baseURLString = baseURLString
    }

    public var baseURL: URL? {
        URL(string: baseURLString)
    }
}

public struct MobileConnectionDraft: Equatable, Sendable {
    public let baseURLString: String
    public let token: String

    public init(baseURLString: String, token: String) {
        self.baseURLString = baseURLString
        self.token = token
    }
}
