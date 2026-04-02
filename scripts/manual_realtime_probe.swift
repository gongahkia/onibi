#!/usr/bin/env swift

import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

let commandLineArguments = Array(CommandLine.arguments.dropFirst())
if commandLineArguments.contains("--help") {
    print(Arguments.usage)
    exit(0)
}

do {
    let arguments = try Arguments.parse(commandLineArguments)
    try await ProbeRunner(arguments: arguments).run()
} catch {
    fputs("error: \(error.localizedDescription)\n", stderr)
    exit(1)
}

private struct ProbeRunner {
    let arguments: Arguments

    func run() async throws {
        let bootstrap = try await fetchBootstrap()
        let targetSessionID = arguments.sessionID ?? bootstrap.sessions.first?["id"] as? String

        print("bootstrap:")
        print(prettyPrinted(bootstrap.rawObject))

        guard let websocketURL = websocketURL(from: arguments.baseURL) else {
            throw ProbeError.invalidBaseURL(arguments.baseURL.absoluteString)
        }

        let websocket = URLSession.shared.webSocketTask(with: websocketURL)
        websocket.resume()

        let receiver = Task {
            await receiveLoop(on: websocket)
        }

        try await send(
            [
                "type": "auth",
                "token": arguments.token
            ],
            over: websocket
        )

        try await Task.sleep(nanoseconds: 200_000_000)

        if arguments.subscribe, let targetSessionID {
            try await send(
                [
                    "type": "subscribe",
                    "sessionId": targetSessionID
                ],
                over: websocket
            )
        }

        if arguments.requestBuffer, let targetSessionID {
            try await send(
                [
                    "type": "request_buffer",
                    "sessionId": targetSessionID
                ],
                over: websocket
            )
        }

        if let text = arguments.text, let targetSessionID {
            try await send(
                [
                    "type": "send_input",
                    "sessionId": targetSessionID,
                    "kind": "text",
                    "text": text,
                    "clientRequestId": UUID().uuidString
                ],
                over: websocket
            )
        }

        if let key = arguments.key, let targetSessionID {
            try await send(
                [
                    "type": "send_input",
                    "sessionId": targetSessionID,
                    "kind": "key",
                    "key": key,
                    "clientRequestId": UUID().uuidString
                ],
                over: websocket
            )
        }

        if targetSessionID == nil, arguments.requiresTargetSession {
            print("warning: no controllable session available for subscribe, buffer, or input actions")
        } else if let targetSessionID {
            print("target session: \(targetSessionID)")
        }

        try await Task.sleep(nanoseconds: UInt64(arguments.listenSeconds * 1_000_000_000))
        receiver.cancel()
        websocket.cancel(with: .goingAway, reason: nil)
        _ = await receiver.result
    }

    private func fetchBootstrap() async throws -> BootstrapPayload {
        var request = URLRequest(url: arguments.baseURL.appending(path: "/api/v2/bootstrap"))
        request.addValue("Bearer \(arguments.token)", forHTTPHeaderField: "Authorization")

        let (data, response) = try await URLSession.shared.data(for: request)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw ProbeError.invalidHTTPResponse
        }

        guard httpResponse.statusCode == 200 else {
            let body = String(data: data, encoding: .utf8) ?? "<non-utf8>"
            throw ProbeError.unsuccessfulBootstrap(statusCode: httpResponse.statusCode, body: body)
        }

        let object = try JSONSerialization.jsonObject(with: data, options: [])
        guard let dictionary = object as? [String: Any] else {
            throw ProbeError.invalidBootstrapPayload
        }

        let sessions = dictionary["sessions"] as? [[String: Any]] ?? []
        return BootstrapPayload(rawObject: dictionary, sessions: sessions)
    }

    private func receiveLoop(on websocket: URLSessionWebSocketTask) async {
        while !Task.isCancelled {
            do {
                let message = try await websocket.receive()
                switch message {
                case .string(let string):
                    if let data = string.data(using: .utf8),
                       let object = try? JSONSerialization.jsonObject(with: data, options: []) {
                        print("realtime:")
                        print(prettyPrinted(object))
                    } else {
                        print("realtime:")
                        print(string)
                    }
                case .data(let data):
                    print("realtime binary frame (\(data.count) bytes)")
                @unknown default:
                    print("realtime unknown frame")
                }
            } catch {
                if !Task.isCancelled {
                    print("receive loop stopped: \(error.localizedDescription)")
                }
                return
            }
        }
    }

    private func send(
        _ object: [String: Any],
        over websocket: URLSessionWebSocketTask
    ) async throws {
        let data = try JSONSerialization.data(withJSONObject: object, options: [.sortedKeys])
        guard let text = String(data: data, encoding: .utf8) else {
            throw ProbeError.invalidOutboundPayload
        }
        print("send:")
        print(text)
        try await websocket.send(.string(text))
    }

    private func websocketURL(from baseURL: URL) -> URL? {
        guard var components = URLComponents(url: baseURL, resolvingAgainstBaseURL: false) else {
            return nil
        }
        switch components.scheme?.lowercased() {
        case "http":
            components.scheme = "ws"
        case "https":
            components.scheme = "wss"
        default:
            return nil
        }
        components.path = "/api/v2/realtime"
        components.query = nil
        return components.url
    }

    private func prettyPrinted(_ object: Any) -> String {
        guard JSONSerialization.isValidJSONObject(object),
              let data = try? JSONSerialization.data(withJSONObject: object, options: [.prettyPrinted, .sortedKeys]),
              let string = String(data: data, encoding: .utf8) else {
            return String(describing: object)
        }
        return string
    }
}

private struct BootstrapPayload {
    let rawObject: [String: Any]
    let sessions: [[String: Any]]
}

private struct Arguments {
    static let usage = """
    usage: swift scripts/manual_realtime_probe.swift [--base-url http://127.0.0.1:8787] [--token TOKEN] [--session-id ID] [--subscribe] [--request-buffer] [--text "pwd"] [--key enter] [--listen-seconds 5]
    """

    let baseURL: URL
    let token: String
    let sessionID: String?
    let subscribe: Bool
    let requestBuffer: Bool
    let text: String?
    let key: String?
    let listenSeconds: Double

    var requiresTargetSession: Bool {
        subscribe || requestBuffer || text != nil || key != nil
    }

    static func parse(_ arguments: [String]) throws -> Arguments {
        var baseURL = URL(string: "http://127.0.0.1:8787")
        var token = ProcessInfo.processInfo.environment["ONIBI_PAIRING_TOKEN"]
        var sessionID: String?
        var subscribe = false
        var requestBuffer = false
        var text: String?
        var key: String?
        var listenSeconds = 3.0

        var iterator = arguments.makeIterator()
        while let argument = iterator.next() {
            switch argument {
            case "--base-url":
                baseURL = URL(string: try nextValue(after: argument, iterator: &iterator))
            case "--token":
                token = try nextValue(after: argument, iterator: &iterator)
            case "--session-id":
                sessionID = try nextValue(after: argument, iterator: &iterator)
            case "--text":
                text = try nextValue(after: argument, iterator: &iterator)
            case "--key":
                key = try nextValue(after: argument, iterator: &iterator)
            case "--listen-seconds":
                let rawValue = try nextValue(after: argument, iterator: &iterator)
                guard let parsed = Double(rawValue), parsed > 0 else {
                    throw ProbeError.invalidArgument(argument)
                }
                listenSeconds = parsed
            case "--subscribe":
                subscribe = true
            case "--request-buffer":
                requestBuffer = true
            default:
                throw ProbeError.invalidArgument(argument)
            }
        }

        guard let baseURL else {
            throw ProbeError.invalidBaseURL("<nil>")
        }

        guard let token, !token.isEmpty else {
            throw ProbeError.missingToken
        }

        return Arguments(
            baseURL: baseURL,
            token: token,
            sessionID: sessionID,
            subscribe: subscribe,
            requestBuffer: requestBuffer,
            text: text,
            key: key,
            listenSeconds: listenSeconds
        )
    }

    private static func nextValue(
        after flag: String,
        iterator: inout IndexingIterator<[String]>
    ) throws -> String {
        guard let value = iterator.next(), !value.isEmpty else {
            throw ProbeError.invalidArgument(flag)
        }
        return value
    }
}

private enum ProbeError: LocalizedError {
    case invalidArgument(String)
    case invalidBaseURL(String)
    case invalidBootstrapPayload
    case invalidHTTPResponse
    case invalidOutboundPayload
    case missingToken
    case unsuccessfulBootstrap(statusCode: Int, body: String)
    case usage

    var errorDescription: String? {
        switch self {
        case .invalidArgument(let argument):
            return "invalid argument: \(argument)"
        case .invalidBaseURL(let value):
            return "invalid base URL: \(value)"
        case .invalidBootstrapPayload:
            return "bootstrap response was not a JSON object"
        case .invalidHTTPResponse:
            return "bootstrap request did not return an HTTP response"
        case .invalidOutboundPayload:
            return "failed to encode websocket payload"
        case .missingToken:
            return """
            missing pairing token. Pass --token <value> or set ONIBI_PAIRING_TOKEN.
            \(Arguments.usage)
            """
        case .unsuccessfulBootstrap(let statusCode, let body):
            return "bootstrap request failed with HTTP \(statusCode): \(body)"
        case .usage:
            return Arguments.usage
        }
    }
}
