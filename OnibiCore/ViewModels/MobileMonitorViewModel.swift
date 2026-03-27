import Foundation
import Observation

@MainActor
@Observable
public final class MobileMonitorViewModel {
    public enum ConnectionState: Equatable {
        case idle
        case notConfigured
        case loading
        case online
        case unauthorized
        case unreachable
        case failed(String)
    }

    public private(set) var connectionState: ConnectionState
    public private(set) var hasConfiguration: Bool
    public private(set) var health: HostHealth?
    public private(set) var summary: SummaryResponse?
    public private(set) var sessions: [SessionSnapshot] = []
    public private(set) var recentEvents: [EventPreview] = []
    public private(set) var sessionDetails: [String: SessionDetail] = [:]
    public private(set) var loadingSessionIDs = Set<String>()
    public private(set) var isRefreshing = false
    public private(set) var lastRefreshAt: Date?
    public private(set) var connectionDraft: MobileConnectionDraft?

    @ObservationIgnored
    private let client: MobileAPIClientProtocol
    @ObservationIgnored
    private let connectionStore: MobileConnectionStore
    @ObservationIgnored
    private let pollInterval: TimeInterval
    @ObservationIgnored
    private var pollingTask: Task<Void, Never>?
    @ObservationIgnored
    private var latestCursor: Date?

    public init(
        client: MobileAPIClientProtocol = MobileAPIClient(),
        connectionStore: MobileConnectionStore = MobileConnectionStore(),
        pollInterval: TimeInterval = 5
    ) {
        self.client = client
        self.connectionStore = connectionStore
        self.pollInterval = pollInterval
        let storedDraft = Self.makeConnectionDraft(from: connectionStore.loadConfiguration())
        self.connectionDraft = storedDraft
        self.hasConfiguration = storedDraft != nil
        self.connectionState = storedDraft == nil ? .notConfigured : .idle
    }

    public var isConfigured: Bool {
        hasConfiguration
    }

    public func sessionDetail(for id: String) -> SessionDetail? {
        sessionDetails[id]
    }

    public func isLoadingSessionDetail(_ id: String) -> Bool {
        loadingSessionIDs.contains(id)
    }

    public func saveConfiguration(baseURLString: String, token: String) throws {
        try connectionStore.saveConfiguration(baseURLString: baseURLString, token: token)
        connectionDraft = MobileConnectionDraft(baseURLString: baseURLString, token: token)
        hasConfiguration = true
        connectionState = .idle
        resetRuntimeState()
    }

    public func clearConfiguration() {
        connectionStore.clear()
        pollingTask?.cancel()
        pollingTask = nil
        connectionDraft = nil
        hasConfiguration = false
        connectionState = .notConfigured
        resetRuntimeState()
    }

    public func setSceneActive(_ isActive: Bool) {
        if isActive {
            startPolling()
        } else {
            stopPolling()
        }
    }

    public func startPolling() {
        guard isConfigured else {
            connectionState = .notConfigured
            return
        }

        stopPolling()
        pollingTask = Task { [weak self] in
            guard let self else { return }
            await self.refresh()
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: UInt64(self.pollInterval * 1_000_000_000))
                await self.refresh()
            }
        }
    }

    public func stopPolling() {
        pollingTask?.cancel()
        pollingTask = nil
    }

    public func refresh() async {
        guard isConfigured else {
            connectionState = .notConfigured
            return
        }

        let isInitialLoad = health == nil && summary == nil && sessions.isEmpty && recentEvents.isEmpty
        if isInitialLoad {
            connectionState = .loading
        }
        isRefreshing = true

        do {
            async let nextHealth = client.fetchHealth()
            async let nextSummary = client.fetchSummary()
            async let nextSessions = client.fetchSessions()
            async let nextEvents = client.fetchEvents(cursor: latestCursor, limit: 25)

            let (resolvedHealth, resolvedSummary, resolvedSessions, resolvedEvents) = try await (
                nextHealth,
                nextSummary,
                nextSessions,
                nextEvents
            )

            health = resolvedHealth
            summary = resolvedSummary
            sessions = resolvedSessions
            syncSessionDetails(with: resolvedSessions)

            if let newest = resolvedEvents.max(by: { $0.timestamp < $1.timestamp }) {
                latestCursor = newest.timestamp
            }

            recentEvents = mergeRecentEvents(resolvedEvents)

            connectionState = .online
            lastRefreshAt = Date()
        } catch let error as MobileClientError {
            connectionState = Self.connectionState(for: error)
        } catch {
            connectionState = .failed(error.localizedDescription)
        }

        isRefreshing = false
    }

    public func loadSessionDetail(id: String, forceRefresh: Bool = false) async {
        guard isConfigured else {
            connectionState = .notConfigured
            return
        }

        if !forceRefresh, sessionDetails[id] != nil {
            return
        }

        loadingSessionIDs.insert(id)
        defer {
            loadingSessionIDs.remove(id)
        }

        do {
            let detail = try await client.fetchSessionDetail(id: id)
            sessionDetails[id] = detail
        } catch let error as MobileClientError {
            connectionState = Self.connectionState(for: error)
        } catch {
            connectionState = .failed(error.localizedDescription)
        }
    }
}

private extension MobileMonitorViewModel {
    func mergeRecentEvents(_ incomingEvents: [EventPreview]) -> [EventPreview] {
        let combinedEvents = (incomingEvents + recentEvents)
            .sorted(by: { $0.timestamp > $1.timestamp })

        var uniqueIDs = Set<UUID>()
        var mergedEvents: [EventPreview] = []
        mergedEvents.reserveCapacity(min(combinedEvents.count, 50))

        for event in combinedEvents where uniqueIDs.insert(event.id).inserted {
            mergedEvents.append(event)
            if mergedEvents.count == 50 {
                break
            }
        }

        return mergedEvents
    }

    func resetRuntimeState() {
        health = nil
        summary = nil
        sessions = []
        recentEvents = []
        sessionDetails = [:]
        loadingSessionIDs = []
        isRefreshing = false
        lastRefreshAt = nil
        latestCursor = nil
    }

    func syncSessionDetails(with updatedSessions: [SessionSnapshot]) {
        let validIDs = Set(updatedSessions.map(\.id))
        sessionDetails = sessionDetails.filter { validIDs.contains($0.key) }

        for session in updatedSessions {
            guard let existingDetail = sessionDetails[session.id] else {
                continue
            }

            sessionDetails[session.id] = SessionDetail(
                session: session,
                commands: existingDetail.commands
            )
        }
    }

    static func connectionState(for error: MobileClientError) -> ConnectionState {
        switch error {
        case .notConfigured:
            return .notConfigured
        case .unauthorized:
            return .unauthorized
        case .unreachable:
            return .unreachable
        default:
            return .failed(error.localizedDescription)
        }
    }

    static func makeConnectionDraft(
        from storedConnection: (configuration: MobileConnectionConfiguration, token: String)?
    ) -> MobileConnectionDraft? {
        guard let storedConnection else {
            return nil
        }

        return MobileConnectionDraft(
            baseURLString: storedConnection.configuration.baseURLString,
            token: storedConnection.token
        )
    }
}
