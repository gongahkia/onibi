import Foundation

enum TerminalCommandLifecycleEvent: Equatable {
    case start(command: String, workingDirectory: String?)
    case end(exitCode: Int?, workingDirectory: String?)
}

struct TerminalCommandLifecycleParser {
    private var buffer: [UInt8] = []
    private let maxBufferedBytes: Int

    init(maxBufferedBytes: Int = 8192) {
        self.maxBufferedBytes = max(32, maxBufferedBytes)
    }

    mutating func consume(_ data: Data) -> [TerminalCommandLifecycleEvent] {
        guard !data.isEmpty else {
            return []
        }

        buffer.append(contentsOf: data)
        var events: [TerminalCommandLifecycleEvent] = []

        while true {
            guard let escapeIndex = buffer.firstIndex(of: 0x1B) else {
                buffer.removeAll(keepingCapacity: true)
                break
            }

            if escapeIndex > 0 {
                buffer.removeFirst(escapeIndex)
            }

            guard buffer.count >= 2 else {
                break
            }

            guard buffer[1] == 0x5D else {
                buffer.removeFirst()
                continue
            }

            guard let terminator = findTerminator(startingAt: 2) else {
                if buffer.count > maxBufferedBytes {
                    buffer.removeAll(keepingCapacity: true)
                }
                break
            }

            let payload = Array(buffer[2..<terminator.payloadEnd])
            if let event = parseEvent(from: payload) {
                events.append(event)
            }

            buffer.removeFirst(terminator.sequenceEnd)
        }

        return events
    }

    private func findTerminator(startingAt start: Int) -> (payloadEnd: Int, sequenceEnd: Int)? {
        guard start < buffer.count else {
            return nil
        }

        var index = start
        while index < buffer.count {
            if buffer[index] == 0x07 {
                return (payloadEnd: index, sequenceEnd: index + 1)
            }
            if buffer[index] == 0x1B {
                guard index + 1 < buffer.count else {
                    return nil
                }
                if buffer[index + 1] == 0x5C {
                    return (payloadEnd: index, sequenceEnd: index + 2)
                }
            }
            index += 1
        }
        return nil
    }

    private func parseEvent(from payload: [UInt8]) -> TerminalCommandLifecycleEvent? {
        guard let payloadString = String(bytes: payload, encoding: .utf8) else {
            return nil
        }

        let parts = payloadString.split(separator: ";", omittingEmptySubsequences: false).map(String.init)
        guard parts.count >= 2, parts[0] == "1337" else {
            return nil
        }

        let fields = parseFields(parts.dropFirst(2))
        switch parts[1] {
        case "OnibiCommandStart":
            guard let command = decodeBase64Field(fields["command"]), !command.isEmpty else {
                return nil
            }
            return .start(command: command, workingDirectory: decodeBase64Field(fields["cwd"]))
        case "OnibiCommandEnd":
            let exitCode = fields["exit"].flatMap(Int.init)
            return .end(exitCode: exitCode, workingDirectory: decodeBase64Field(fields["cwd"]))
        default:
            return nil
        }
    }

    private func parseFields(_ rawFields: ArraySlice<String>) -> [String: String] {
        var fields: [String: String] = [:]
        for rawField in rawFields {
            let pair = rawField.split(separator: "=", maxSplits: 1, omittingEmptySubsequences: false)
            guard pair.count == 2 else {
                continue
            }
            fields[String(pair[0])] = String(pair[1])
        }
        return fields
    }

    private func decodeBase64Field(_ value: String?) -> String? {
        guard let value, let data = Data(base64Encoded: value) else {
            return nil
        }
        return String(data: data, encoding: .utf8)
    }
}
