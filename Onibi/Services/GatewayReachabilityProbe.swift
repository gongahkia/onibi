import Foundation

enum GatewayProbeOutcome: Equatable, Sendable {
    case ok(latencyMs: Int)
    case badStatus(Int)
    case refused
    case timeout
    case tlsError(String)
    case invalidURL
    case other(String)

    var isOK: Bool {
        if case .ok = self { return true }
        return false
    }

    var label: String {
        switch self {
        case .ok(let ms): return "reachable (\(ms) ms)"
        case .badStatus(let code): return "HTTP \(code)"
        case .refused: return "connection refused"
        case .timeout: return "timed out"
        case .tlsError(let detail): return "TLS error: \(detail)"
        case .invalidURL: return "invalid URL"
        case .other(let detail): return detail
        }
    }
}

struct GatewayProbeResult: Identifiable, Sendable {
    let id = UUID()
    let url: String
    let outcome: GatewayProbeOutcome
}

/// Probes `/api/v2/health` against a list of URLs. Unauthenticated — the route
/// was added to MobileGatewayRouter for exactly this purpose.
actor GatewayReachabilityProbe {
    static let shared = GatewayReachabilityProbe()

    private let session: URLSession

    init(timeout: TimeInterval = 3.0) {
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = timeout
        config.timeoutIntervalForResource = timeout + 2
        config.waitsForConnectivity = false
        self.session = URLSession(configuration: config)
    }

    func probeAll(baseURLs: [String]) async -> [GatewayProbeResult] {
        await withTaskGroup(of: GatewayProbeResult.self) { group in
            for url in baseURLs {
                group.addTask {
                    let outcome = await Self.probeOne(url: url, session: self.session)
                    return GatewayProbeResult(url: url, outcome: outcome)
                }
            }

            var results: [GatewayProbeResult] = []
            for await result in group {
                results.append(result)
            }
            // Preserve input order when possible.
            return baseURLs.compactMap { url in
                results.first { $0.url == url }
            }
        }
    }

    static func probeOne(url baseURL: String, session: URLSession) async -> GatewayProbeOutcome {
        let trimmed = baseURL.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let parsed = URL(string: trimmed),
              let scheme = parsed.scheme?.lowercased(),
              scheme == "http" || scheme == "https",
              parsed.host != nil
        else {
            return .invalidURL
        }
        guard let url = URL(string: trimmed + "/api/v2/health") else {
            return .invalidURL
        }

        let started = Date()
        do {
            let (data, response) = try await session.data(from: url)
            let elapsedMs = Int(Date().timeIntervalSince(started) * 1000)
            guard let http = response as? HTTPURLResponse else {
                return .other("non-HTTP response")
            }
            if http.statusCode == 200 {
                // Best-effort confirm body is {"status":"ok"}
                if let text = String(data: data, encoding: .utf8), text.contains("\"status\"") {
                    return .ok(latencyMs: elapsedMs)
                }
                return .ok(latencyMs: elapsedMs)
            }
            return .badStatus(http.statusCode)
        } catch let error as URLError {
            switch error.code {
            case .timedOut:
                return .timeout
            case .cannotConnectToHost, .cannotFindHost, .networkConnectionLost:
                return .refused
            case .secureConnectionFailed, .serverCertificateUntrusted, .clientCertificateRejected:
                return .tlsError(error.localizedDescription)
            default:
                return .other(error.localizedDescription)
            }
        } catch {
            return .other(error.localizedDescription)
        }
    }
}
