import Foundation

public protocol MobileGatewayDataProvider: Sendable {
    func health() async throws -> HostHealth
    func summary() async throws -> SummaryResponse
    func sessions() async throws -> [SessionSnapshot]
    func session(id: String) async throws -> SessionDetail?
    func events(after cursor: Date?, limit: Int) async throws -> [EventPreview]
    func diagnostics() async throws -> DiagnosticsResponse
    func featureFlags() async throws -> FeatureFlagsResponse
    func controllableSessions() async throws -> [ControllableSessionSnapshot]
    func sessionOutputBuffer(id: String) async throws -> SessionOutputBufferSnapshot?
    func sendInput(to sessionId: String, payload: RemoteInputPayload) async throws -> RemoteInputAcceptance?
}

public extension MobileGatewayDataProvider {
    func featureFlags() async throws -> FeatureFlagsResponse {
        FeatureFlagsResponse(
            legacyMonitoringEnabled: true,
            remoteControlEnabled: false,
            realtimeSessionsEnabled: false,
            websocketEnabled: false,
            fallbackInputEnabled: false
        )
    }

    func controllableSessions() async throws -> [ControllableSessionSnapshot] {
        []
    }

    func sessionOutputBuffer(id: String) async throws -> SessionOutputBufferSnapshot? {
        nil
    }

    func sendInput(to sessionId: String, payload: RemoteInputPayload) async throws -> RemoteInputAcceptance? {
        nil
    }

    func bootstrap() async throws -> GatewayBootstrapResponse {
        GatewayBootstrapResponse(
            health: try await health(),
            featureFlags: try await featureFlags(),
            sessions: try await controllableSessions(),
            diagnostics: try await diagnostics()
        )
    }
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
        headers: [String: String] = [:],
        body: Data = Data()
    ) async -> MobileGatewayResponse {
        let normalizedMethod = method.uppercased()

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
            switch (normalizedMethod, path) {
            case ("GET", "/api/v1/health"):
                return try jsonResponse(statusCode: 200, encodable: await dataProvider.health())
            case ("GET", "/api/v1/summary"):
                return try jsonResponse(statusCode: 200, encodable: await dataProvider.summary())
            case ("GET", "/api/v1/sessions"):
                return try jsonResponse(statusCode: 200, encodable: await dataProvider.sessions())
            case ("GET", let sessionPath) where sessionPath.hasPrefix("/api/v1/sessions/"):
                let sessionId = String(sessionPath.dropFirst("/api/v1/sessions/".count))
                guard let detail = try await dataProvider.session(id: sessionId) else {
                    return jsonResponse(statusCode: 404, body: ["error": "session_not_found"])
                }
                return try jsonResponse(statusCode: 200, encodable: detail)
            case ("GET", "/api/v1/events"):
                let cursor = parseCursor(from: queryItems)
                let limit = parseLimit(from: queryItems)
                let events = try await dataProvider.events(after: cursor, limit: limit)
                return try jsonResponse(statusCode: 200, encodable: events)
            case ("GET", "/api/v1/diagnostics"):
                return try jsonResponse(statusCode: 200, encodable: await dataProvider.diagnostics())
            case ("GET", "/api/v2/bootstrap"):
                return try jsonResponse(statusCode: 200, encodable: await dataProvider.bootstrap())
            case ("GET", "/api/v2/sessions"):
                return try jsonResponse(statusCode: 200, encodable: await dataProvider.controllableSessions())
            case ("GET", let bufferPath) where bufferPath.hasPrefix("/api/v2/sessions/") && bufferPath.hasSuffix("/buffer"):
                guard let sessionId = parseSessionIdentifier(from: bufferPath, suffix: "/buffer") else {
                    return jsonResponse(statusCode: 404, body: ["error": "not_found"])
                }
                guard let snapshot = try await dataProvider.sessionOutputBuffer(id: sessionId) else {
                    return jsonResponse(statusCode: 404, body: ["error": "session_not_found"])
                }
                return try jsonResponse(statusCode: 200, encodable: snapshot)
            case ("POST", let inputPath) where inputPath.hasPrefix("/api/v2/sessions/") && inputPath.hasSuffix("/input"):
                guard let sessionId = parseSessionIdentifier(from: inputPath, suffix: "/input") else {
                    return jsonResponse(statusCode: 404, body: ["error": "not_found"])
                }
                let payload = try decodeRemoteInputPayload(from: body)
                guard payload.isValid else {
                    return jsonResponse(statusCode: 400, body: ["error": "invalid_input_payload"])
                }
                guard let acceptance = try await dataProvider.sendInput(to: sessionId, payload: payload) else {
                    return jsonResponse(statusCode: 404, body: ["error": "session_not_found"])
                }
                return try jsonResponse(statusCode: 200, encodable: acceptance)
            default:
                if normalizedMethod != "GET" && normalizedMethod != "POST" {
                    return jsonResponse(statusCode: 405, body: ["error": "method_not_allowed"])
                }
                return jsonResponse(statusCode: 404, body: ["error": "not_found"])
            }
        } catch let error as RemoteControlError {
            switch error {
            case .sessionNotFound:
                return jsonResponse(statusCode: 404, body: ["error": "session_not_found"])
            case .sessionNotControllable:
                return jsonResponse(statusCode: 409, body: ["error": "session_not_controllable"])
            case .inputUnavailable:
                return jsonResponse(statusCode: 409, body: ["error": "input_unavailable"])
            case .invalidInputPayload:
                return jsonResponse(statusCode: 400, body: ["error": "invalid_input_payload"])
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

    private func parseSessionIdentifier(from path: String, suffix: String) -> String? {
        let prefix = "/api/v2/sessions/"
        guard path.hasPrefix(prefix), path.hasSuffix(suffix) else {
            return nil
        }

        let sessionId = path
            .dropFirst(prefix.count)
            .dropLast(suffix.count)

        guard !sessionId.isEmpty else {
            return nil
        }

        return String(sessionId)
    }

    private func decodeRemoteInputPayload(from body: Data) throws -> RemoteInputPayload {
        guard !body.isEmpty else {
            throw RemoteControlError.invalidInputPayload
        }

        do {
            return try JSONDateCodec.decoder.decode(RemoteInputPayload.self, from: body)
        } catch {
            throw RemoteControlError.invalidInputPayload
        }
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
