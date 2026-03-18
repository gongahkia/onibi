import Foundation

public enum MobileClientError: LocalizedError, Equatable {
    case notConfigured
    case invalidBaseURL
    case invalidResponse
    case unauthorized
    case unreachable
    case server(statusCode: Int)
    case decodingFailed

    public var errorDescription: String? {
        switch self {
        case .notConfigured:
            return "Mobile connection is not configured."
        case .invalidBaseURL:
            return "The configured mobile URL is invalid."
        case .invalidResponse:
            return "The mobile gateway returned an invalid response."
        case .unauthorized:
            return "The pairing token was rejected by the host."
        case .unreachable:
            return "The host could not be reached."
        case .server(let statusCode):
            return "The host returned status code \(statusCode)."
        case .decodingFailed:
            return "The host response could not be decoded."
        }
    }
}

public protocol MobileAPIClientProtocol: Sendable {
    func fetchHealth() async throws -> HostHealth
    func fetchSummary() async throws -> SummaryResponse
    func fetchSessions() async throws -> [SessionSnapshot]
    func fetchSessionDetail(id: String) async throws -> SessionDetail
    func fetchEvents(cursor: Date?, limit: Int) async throws -> [EventPreview]
}

public final class MobileAPIClient: MobileAPIClientProtocol, @unchecked Sendable {
    private let session: URLSession
    private let connectionStore: MobileConnectionStore

    public init(
        session: URLSession = .shared,
        connectionStore: MobileConnectionStore = MobileConnectionStore()
    ) {
        self.session = session
        self.connectionStore = connectionStore
    }

    public func fetchHealth() async throws -> HostHealth {
        try await perform(path: "/api/v1/health")
    }

    public func fetchSummary() async throws -> SummaryResponse {
        try await perform(path: "/api/v1/summary")
    }

    public func fetchSessions() async throws -> [SessionSnapshot] {
        try await perform(path: "/api/v1/sessions")
    }

    public func fetchSessionDetail(id: String) async throws -> SessionDetail {
        try await perform(path: "/api/v1/sessions/\(id)")
    }

    public func fetchEvents(cursor: Date?, limit: Int) async throws -> [EventPreview] {
        var components = URLComponents()
        components.path = "/api/v1/events"
        var queryItems = [URLQueryItem(name: "limit", value: String(limit))]
        if let cursor {
            queryItems.append(URLQueryItem(name: "cursor", value: ISO8601DateFormatter().string(from: cursor)))
        }
        components.queryItems = queryItems
        return try await perform(path: components.string ?? "/api/v1/events")
    }

    private func perform<T: Decodable>(path: String) async throws -> T {
        guard let configuration = connectionStore.loadConfiguration() else {
            throw MobileClientError.notConfigured
        }
        guard let baseURL = configuration.configuration.baseURL else {
            throw MobileClientError.invalidBaseURL
        }

        let targetURL: URL
        if let url = URL(string: path, relativeTo: baseURL) {
            targetURL = url
        } else {
            throw MobileClientError.invalidBaseURL
        }

        var request = URLRequest(url: targetURL)
        request.httpMethod = "GET"
        request.setValue("Bearer \(configuration.token)", forHTTPHeaderField: "Authorization")
        request.timeoutInterval = 10

        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: request)
        } catch {
            throw MobileClientError.unreachable
        }

        guard let httpResponse = response as? HTTPURLResponse else {
            throw MobileClientError.invalidResponse
        }

        switch httpResponse.statusCode {
        case 200:
            do {
                return try JSONDateCodec.decoder.decode(T.self, from: data)
            } catch {
                throw MobileClientError.decodingFailed
            }
        case 401:
            throw MobileClientError.unauthorized
        default:
            throw MobileClientError.server(statusCode: httpResponse.statusCode)
        }
    }
}
