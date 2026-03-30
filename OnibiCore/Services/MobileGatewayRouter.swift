import Foundation

public protocol MobileGatewayDataProvider: Sendable {
    func health() async throws -> HostHealth
    func summary() async throws -> SummaryResponse
    func sessions() async throws -> [SessionSnapshot]
    func session(id: String) async throws -> SessionDetail?
    func events(after cursor: Date?, limit: Int) async throws -> [EventPreview]
    func diagnostics() async throws -> DiagnosticsResponse
}

public struct MobileGatewayResponse: Sendable {
    public let statusCode: Int
    public let headers: [String: String]
    public let body: Data

    public init(statusCode: Int, headers: [String: String] = [:], body: Data = Data()) {
        self.statusCode = statusCode
        self.headers = headers
        self.body = body
    }
}

public struct MobileGatewayRouter: Sendable {
    private let tokenProvider: @Sendable () throws -> String?
    private let dataProvider: MobileGatewayDataProvider

    public init(
        tokenProvider: @escaping @Sendable () throws -> String?,
        dataProvider: MobileGatewayDataProvider
    ) {
        self.tokenProvider = tokenProvider
        self.dataProvider = dataProvider
    }

    public func route(
        method: String,
        path: String,
        queryItems: [URLQueryItem] = [],
        headers: [String: String] = [:]
    ) async -> MobileGatewayResponse {
        guard method.uppercased() == "GET" else {
            return jsonResponse(statusCode: 405, body: ["error": "method_not_allowed"])
        }

        do {
            guard try authorize(headers: headers) else {
                return jsonResponse(statusCode: 401, body: ["error": "unauthorized"])
            }
        } catch {
            return jsonResponse(
                statusCode: 500,
                body: [
                    "error": "authorization_provider_failure",
                    "reason": error.localizedDescription
                ]
            )
        }

        do {
            switch path {
            case "/api/v1/health":
                return try jsonResponse(statusCode: 200, encodable: await dataProvider.health())
            case "/api/v1/summary":
                return try jsonResponse(statusCode: 200, encodable: await dataProvider.summary())
            case "/api/v1/sessions":
                return try jsonResponse(statusCode: 200, encodable: await dataProvider.sessions())
            case let sessionPath where sessionPath.hasPrefix("/api/v1/sessions/"):
                let sessionId = String(sessionPath.dropFirst("/api/v1/sessions/".count))
                guard let detail = try await dataProvider.session(id: sessionId) else {
                    return jsonResponse(statusCode: 404, body: ["error": "session_not_found"])
                }
                return try jsonResponse(statusCode: 200, encodable: detail)
            case "/api/v1/events":
                let cursor = parseCursor(from: queryItems)
                let limit = parseLimit(from: queryItems)
                let events = try await dataProvider.events(after: cursor, limit: limit)
                return try jsonResponse(statusCode: 200, encodable: events)
            case "/api/v1/diagnostics":
                return try jsonResponse(statusCode: 200, encodable: await dataProvider.diagnostics())
            default:
                return jsonResponse(statusCode: 404, body: ["error": "not_found"])
            }
        } catch {
            return jsonResponse(
                statusCode: 500,
                body: [
                    "error": "internal_error",
                    "reason": error.localizedDescription
                ]
            )
        }
    }

    private func authorize(headers: [String: String]) throws -> Bool {
        guard
            let expectedToken = try tokenProvider(),
            !expectedToken.isEmpty
        else {
            return false
        }

        let authorization = headers.first {
            $0.key.caseInsensitiveCompare("Authorization") == .orderedSame
        }?.value ?? ""

        return authorization == "Bearer \(expectedToken)"
    }

    private func parseCursor(from queryItems: [URLQueryItem]) -> Date? {
        guard let cursorValue = queryItems.first(where: { $0.name == "cursor" })?.value else {
            return nil
        }

        return JSONDateCodec.decode(cursorValue)
    }

    private func parseLimit(from queryItems: [URLQueryItem]) -> Int {
        let rawValue = queryItems.first(where: { $0.name == "limit" })?.value
        let limit = rawValue.flatMap(Int.init) ?? 20
        return min(max(limit, 1), 200)
    }

    private func jsonResponse<T: Encodable>(statusCode: Int, encodable: T) throws -> MobileGatewayResponse {
        let data = try JSONDateCodec.encoder.encode(encodable)
        return MobileGatewayResponse(
            statusCode: statusCode,
            headers: [
                "Content-Type": "application/json; charset=utf-8",
                "Content-Length": "\(data.count)"
            ],
            body: data
        )
    }

    private func jsonResponse(statusCode: Int, body: [String: String]) -> MobileGatewayResponse {
        let data: Data
        do {
            data = try JSONSerialization.data(withJSONObject: body, options: [.sortedKeys])
        } catch {
            data = Data("{\"error\":\"response_serialization_failed\"}".utf8)
        }
        return MobileGatewayResponse(
            statusCode: statusCode,
            headers: [
                "Content-Type": "application/json; charset=utf-8",
                "Content-Length": "\(data.count)"
            ],
            body: data
        )
    }
}

public enum JSONDateCodec {
    public static let encoder: JSONEncoder = {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }()

    public static let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }()

    private static let isoFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()

    private static let fallbackFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter
    }()

    public static func decode(_ value: String) -> Date? {
        if let exact = isoFormatter.date(from: value) {
            return exact
        }
        return fallbackFormatter.date(from: value)
    }
}
