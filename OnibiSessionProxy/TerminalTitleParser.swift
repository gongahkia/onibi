import Foundation

struct TerminalTitleParser {
    private var buffer: [UInt8] = []
    private let maxBufferedBytes: Int
    private let maxTitleBytes: Int

    init(maxBufferedBytes: Int = 8192, maxTitleBytes: Int = 512) {
        self.maxBufferedBytes = max(32, maxBufferedBytes)
        self.maxTitleBytes = max(1, maxTitleBytes)
    }

    mutating func consume(_ data: Data) -> [String] {
        guard !data.isEmpty else {
            return []
        }

        buffer.append(contentsOf: data)
        var titles: [String] = []

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
            if let title = parseTitle(from: payload) {
                titles.append(title)
            }

            buffer.removeFirst(terminator.sequenceEnd)
        }

        return titles
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

    private func parseTitle(from payload: [UInt8]) -> String? {
        guard let separatorIndex = payload.firstIndex(of: 0x3B) else {
            return nil
        }

        let command = String(bytes: payload[..<separatorIndex], encoding: .utf8)
        guard command == "0" || command == "2" else {
            return nil
        }

        let titleBytes = payload[payload.index(after: separatorIndex)...].prefix(maxTitleBytes)
        guard let title = String(bytes: titleBytes, encoding: .utf8) else {
            return nil
        }
        return title.trimmingCharacters(in: .controlCharacters)
    }
}
