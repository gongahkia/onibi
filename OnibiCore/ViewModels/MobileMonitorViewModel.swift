import Combine
import Foundation

@MainActor
public final class MobileMonitorViewModel: ObservableObject {
    public enum ConnectionState: Equatable {
        case idle
        case notConfigured
        case loading
        case online
        case unauthorized
        case unreachable
        case failed(String)
    }

    @Published public private(set) var connectionState: ConnectionState = .idle
    @Published public private(set) var hasConfiguration: Bool
    @Published public private(set) var health: HostHealth?
    @Published public private(set) var summary: SummaryResponse?
    @Published public private(set) var sessions: [SessionSnapshot] = []
    @Published public private(set) var recentEvents: [EventPreview] = []
    @Published public private(set) var selectedSessionDetail: SessionDetail?

    private let client: MobileAPIClientProtocol
    private let connectionStore: MobileConnectionStore
    private let pollInterval: TimeInterval
    private var pollingTask: Task<Void, Never>?
    private var latestCursor: Date?

    public init(
        client: MobileAPIClientProtocol = MobileAPIClient(),
        connectionStore: MobileConnectionStore = MobileConnectionStore(),
        pollInterval: TimeInterval = 5
    ) {
        self.client = client
        self.connectionStore = connectionStore
        self.pollInterval = pollInterval
        self.hasConfiguration = connectionStore.loadConfiguration() != nil
    }

    public var isConfigured: Bool {
        hasConfiguration
    }

    public func saveConfiguration(baseURLString: String, token: String) throws {
        try connectionStore.saveConfiguration(baseURLString: baseURLString, token: token)
        hasConfiguration = true
        connectionState = .idle
    }

    public func clearConfiguration() {
        connectionStore.clear()
        pollingTask?.cancel()
        pollingTask = nil
        hasConfiguration = false
        connectionState = .notConfigured
        health = nil
        summary = nil
        sessions = []
        recentEvents = []
        selectedSessionDetail = nil
        latestCursor = nil
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

        connectionState = .loading
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

            if let newest = resolvedEvents.max(by: { $0.timestamp < $1.timestamp }) {
                latestCursor = newest.timestamp
            }

            if resolvedEvents.isEmpty {
                recentEvents = recentEvents.prefix(50).map { $0 }
            } else {
                recentEvents = Array((resolvedEvents + recentEvents).prefix(50))
            }

            connectionState = .online
        } catch let error as MobileClientError {
            switch error {
            case .notConfigured:
                connectionState = .notConfigured
            case .unauthorized:
                connectionState = .unauthorized
            case .unreachable:
                connectionState = .unreachable
            default:
                connectionState = .failed(error.localizedDescription)
            }
        } catch {
            connectionState = .failed(error.localizedDescription)
        }
    }

    public func loadSessionDetail(id: String) async {
        do {
            selectedSessionDetail = try await client.fetchSessionDetail(id: id)
        } catch let error as MobileClientError {
            connectionState = .failed(error.localizedDescription)
        } catch {
            connectionState = .failed(error.localizedDescription)
        }
    }
}
