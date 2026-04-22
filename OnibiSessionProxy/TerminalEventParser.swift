import Foundation
import OnibiCore

enum TerminalEvent: Equatable {
    case bell
    case workingDirectory(String)
}

struct TerminalEventParser {
    private var buffer: [UInt8] = []
    private let maxBufferedBytes: Int

    init(maxBufferedBytes: Int = 8192) {
        self.maxBufferedBytes = max(32, maxBufferedBytes)
    }

    mutating func consume(_ data: Data) -> [TerminalEvent] {
        guard !data.isEmpty else {
            return []
        }

        buffer.append(contentsOf: data)
        var events: [TerminalEvent] = []

        while !buffer.isEmpty {
            guard let specialIndex = nextSpecialByteIndex() else {
                buffer.removeAll(keepingCapacity: true)
                break
            }

            if specialIndex > 0 {
                buffer.removeFirst(specialIndex)
            }

            if buffer.first == 0x07 {
                events.append(.bell)
                buffer.removeFirst()
                continue
            }

            guard buffer.count >= 2 else {
                break
            }

            guard buffer[0] == 0x1B, buffer[1] == 0x5D else {
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
            if let event = parseOSCEvent(from: payload) {
                events.append(event)
            }
            buffer.removeFirst(terminator.sequenceEnd)
        }

        return events
    }

    private func nextSpecialByteIndex() -> Int? {
        buffer.firstIndex { byte in byte == 0x1B || byte == 0x07 }
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

    private func parseOSCEvent(from payload: [UInt8]) -> TerminalEvent? {
        guard let separatorIndex = payload.firstIndex(of: 0x3B) else {
            return nil
        }

        let command = String(bytes: payload[..<separatorIndex], encoding: .utf8)
        guard command == "7" else {
            return nil
        }

        let rawValue = String(bytes: payload[payload.index(after: separatorIndex)...], encoding: .utf8) ?? ""
        guard let workingDirectory = parseWorkingDirectory(from: rawValue), !workingDirectory.isEmpty else {
            return nil
        }
        return .workingDirectory(workingDirectory)
    }

    private func parseWorkingDirectory(from rawValue: String) -> String? {
        guard rawValue.hasPrefix("file://") else {
            return rawValue.removingPercentEncoding ?? rawValue
        }

        if let url = URL(string: rawValue), url.isFileURL {
            return url.path
        }

        guard let pathStart = rawValue.dropFirst("file://".count).firstIndex(of: "/") else {
            return nil
        }
        let path = rawValue[pathStart...]
        return String(path).removingPercentEncoding ?? String(path)
    }
}
