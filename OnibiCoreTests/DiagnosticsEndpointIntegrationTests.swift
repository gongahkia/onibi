import XCTest
import Network
@testable import OnibiCore

final class DiagnosticsEndpointIntegrationTests: XCTestCase {
    func testDiagnosticsEndpointValidatesAuthAndReturnsRuntimePayload() async throws {
        let token = "integration-token"
        let router = MobileGatewayRouter(
            tokenProvider: { token },
            dataProvider: IntegrationGatewayDataProvider()
        )

        let server = try GatewayIntegrationTestServer(router: router)
        let baseURL = try await server.start()
        defer {
            server.stop()
        }

        let suiteName = "DiagnosticsEndpointIntegrationTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suiteName)!
        defaults.removePersistentDomain(forName: suiteName)
        defer {
            defaults.removePersistentDomain(forName: suiteName)
        }

        let tokenStore = PairingTokenStore(
            service: "test.mobile.integration.\(UUID().uuidString)",
            account: "pairing-token"
        )
        defer {
            tokenStore.clear()
        }

        let connectionStore = MobileConnectionStore(
            defaults: defaults,
            configurationKey: "integration-config",
            tokenStore: tokenStore
        )
        let urlSession = URLSession(configuration: .ephemeral)
        let client = MobileAPIClient(session: urlSession, connectionStore: connectionStore)

        try connectionStore.saveConfiguration(baseURLString: baseURL.absoluteString, token: "invalid-token")

        do {
            _ = try await client.fetchDiagnostics()
            XCTFail("Expected unauthorized error when token is invalid")
        } catch let error as MobileClientError {
            XCTAssertEqual(error, .unauthorized)
        }

        try connectionStore.saveConfiguration(baseURLString: baseURL.absoluteString, token: token)

        let diagnostics = try await client.fetchDiagnostics()
        XCTAssertGreaterThan(diagnostics.diagnosticsEventCount, 0)
        XCTAssertFalse(diagnostics.recentEvents.isEmpty)
        XCTAssertEqual(diagnostics.recentEvents.first?.component, "integration")
    }
}

private struct IntegrationGatewayDataProvider: MobileGatewayDataProvider {
    func health() async throws -> HostHealth {
        HostHealth(
            ghosttyRunning: true,
            schedulerRunning: true,
            lastIngestAt: Date(),
            activeSessionCount: 1,
            gatewayRunning: true
        )
    }

    func summary() async throws -> SummaryResponse {
        SummaryResponse(
            activeSessionCount: 1,
            recentActivityCount: 1,
            latestEventAt: Date()
        )
    }

    func sessions() async throws -> [SessionSnapshot] {
        [
            SessionSnapshot(
                id: "session-1",
                displayName: "session-1",
                isActive: true,
                startedAt: Date().addingTimeInterval(-30),
                lastActivityAt: Date(),
                commandCount: 3,
                primaryAssistant: .codex,
                lastCommandPreview: "codex exec --model gpt-5.4"
            )
        ]
    }

    func session(id: String) async throws -> SessionDetail? {
        let snapshot = try await sessions().first
        guard let snapshot else {
            return nil
        }
        return SessionDetail(session: snapshot, commands: [])
    }

    func events(after cursor: Date?, limit: Int) async throws -> [EventPreview] {
        [
            EventPreview(
                id: UUID(),
                timestamp: Date(),
                sessionId: "session-1",
                assistantKind: .codex,
                kind: .assistantActivity,
                title: "Codex Activity",
                message: "codex exec --model gpt-5.4",
                exitCode: 0
            )
        ]
    }

    func diagnostics() async throws -> DiagnosticsResponse {
        DiagnosticsResponse(
            generatedAt: Date(),
            hostVersion: "integration-test",
            diagnosticsEventCount: 4,
            warningCount: 1,
            errorCount: 1,
            criticalCount: 0,
            schedulerEventsProcessed: 12,
            storageLogCount: 9,
            storageBytes: 4096,
            tailscaleStatus: "serving",
            latestErrorTitle: "Integration warning",
            latestErrorTimestamp: Date().addingTimeInterval(-60),
            recentEvents: [
                DiagnosticsEventPreview(
                    timestamp: Date().addingTimeInterval(-30),
                    component: "integration",
                    severity: .warning,
                    message: "synthetic diagnostics event"
                )
            ]
        )
    }
}

private final class GatewayIntegrationTestServer {
    private let router: MobileGatewayRouter
    private let queue = DispatchQueue(label: "com.onibi.tests.integration-server")
    private var listener: NWListener?

    init(router: MobileGatewayRouter) throws {
        self.router = router
    }

    func start() async throws -> URL {
        let listener = try NWListener(using: .tcp)
        self.listener = listener

        listener.newConnectionHandler = { [weak self] connection in
            self?.handle(connection: connection)
        }

        return try await withCheckedThrowingContinuation { continuation in
            listener.stateUpdateHandler = { state in
                switch state {
                case .ready:
                    guard let port = listener.port else {
                        continuation.resume(throwing: NSError(
                            domain: "GatewayIntegrationTestServer",
                            code: 1,
                            userInfo: [NSLocalizedDescriptionKey: "listener started without an assigned port"]
                        ))
                        return
                    }
                    continuation.resume(returning: URL(string: "http://127.0.0.1:\(port.rawValue)")!)
                case .failed(let error):
                    continuation.resume(throwing: error)
                default:
                    break
                }
            }

            listener.start(queue: queue)
        }
    }

    func stop() {
        listener?.cancel()
        listener = nil
    }

    private func handle(connection: NWConnection) {
        connection.start(queue: queue)
        connection.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) { [weak self] data, _, _, _ in
            guard
                let self,
                let data,
                let request = self.parseRequest(from: data)
            else {
                self?.sendBadRequest(over: connection)
                return
            }

            Task {
                let response = await self.router.route(
                    method: request.method,
                    path: request.path,
                    queryItems: request.queryItems,
                    headers: request.headers
                )
                self.send(response: response, over: connection)
            }
        }
    }

    private func sendBadRequest(over connection: NWConnection) {
        let body = Data("{\"error\":\"bad_request\"}".utf8)
        let response = MobileGatewayResponse(
            statusCode: 400,
            headers: [
                "Content-Type": "application/json; charset=utf-8",
                "Content-Length": "\(body.count)"
            ],
            body: body
        )
        send(response: response, over: connection)
    }

    private func send(response: MobileGatewayResponse, over connection: NWConnection) {
        let reasonPhrase = httpReasonPhrase(for: response.statusCode)
        var responseString = "HTTP/1.1 \(response.statusCode) \(reasonPhrase)\r\n"
        let headers = response.headers.merging(["Connection": "close"]) { existing, _ in existing }
        for (key, value) in headers {
            responseString += "\(key): \(value)\r\n"
        }
        responseString += "\r\n"

        var payload = Data(responseString.utf8)
        payload.append(response.body)
        connection.send(content: payload, completion: .contentProcessed { _ in
            connection.cancel()
        })
    }

    private func parseRequest(from data: Data) -> ParsedRequest? {
        guard let requestString = String(data: data, encoding: .utf8) else {
            return nil
        }

        let headerBlock = requestString.components(separatedBy: "\r\n\r\n").first ?? requestString
        let lines = headerBlock.components(separatedBy: "\r\n")
        guard let requestLine = lines.first else {
            return nil
        }

        let parts = requestLine.split(separator: " ")
        guard parts.count >= 2 else {
            return nil
        }

        let method = String(parts[0])
        let target = String(parts[1])
        let url = URL(string: "http://localhost\(target)")
        let path = url?.path ?? target
        let queryItems = URLComponents(url: url ?? URL(string: "http://localhost")!, resolvingAgainstBaseURL: false)?.queryItems ?? []

        var headers: [String: String] = [:]
        for line in lines.dropFirst() {
            let headerParts = line.split(separator: ":", maxSplits: 1)
            guard headerParts.count == 2 else {
                continue
            }
            headers[String(headerParts[0]).trimmingCharacters(in: .whitespaces)] = String(headerParts[1]).trimmingCharacters(in: .whitespaces)
        }

        return ParsedRequest(method: method, path: path, queryItems: queryItems, headers: headers)
    }

    private func httpReasonPhrase(for statusCode: Int) -> String {
        switch statusCode {
        case 200: return "OK"
        case 400: return "Bad Request"
        case 401: return "Unauthorized"
        case 404: return "Not Found"
        case 405: return "Method Not Allowed"
        case 500: return "Internal Server Error"
        default: return "OK"
        }
    }

    private struct ParsedRequest {
        let method: String
        let path: String
        let queryItems: [URLQueryItem]
        let headers: [String: String]
    }
}
