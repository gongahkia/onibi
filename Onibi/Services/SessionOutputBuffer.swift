import Foundation
import OnibiCore

struct SessionOutputBuffer {
    private(set) var lineLimit: Int
    private(set) var byteLimit: Int
    private(set) var chunks: [SessionOutputChunk] = []
    private(set) var isTruncated = false

    private var totalBytes = 0
    private var totalLines = 0

    init(lineLimit: Int, byteLimit: Int) {
        self.lineLimit = max(1, lineLimit)
        self.byteLimit = max(1, byteLimit)
    }

    var currentCursor: String? {
        chunks.last?.id
    }

    mutating func reconfigure(lineLimit: Int, byteLimit: Int) {
        self.lineLimit = max(1, lineLimit)
        self.byteLimit = max(1, byteLimit)
        trimIfNeeded()
    }

    @discardableResult
    mutating func append(
        sessionId: String,
        stream: SessionOutputStream,
        data: Data,
        timestamp: Date = Date()
    ) -> SessionOutputChunk {
        let normalizedData = normalizedData(from: data)
        let chunk = SessionOutputChunk(
            sessionId: sessionId,
            stream: stream,
            timestamp: timestamp,
            data: normalizedData
        )
        chunks.append(chunk)
        totalBytes += chunk.data.count
        totalLines += estimatedLineCount(in: chunk.data)
        trimIfNeeded()
        return chunk
    }

    func snapshot(for session: ControllableSessionSnapshot) -> SessionOutputBufferSnapshot {
        SessionOutputBufferSnapshot(
            session: session,
            bufferCursor: currentCursor,
            chunks: chunks,
            truncated: isTruncated
        )
    }

    private mutating func trimIfNeeded() {
        while chunks.count > 1 && (totalBytes > byteLimit || totalLines > lineLimit) {
            removeFirstChunk()
            isTruncated = true
        }

        guard
            let lastChunk = chunks.last,
            totalBytes > byteLimit
        else {
            return
        }

        let trimmedData = Data(lastChunk.data.suffix(byteLimit))
        totalBytes -= lastChunk.data.count
        totalLines -= estimatedLineCount(in: lastChunk.data)

        let trimmedChunk = SessionOutputChunk(
            id: lastChunk.id,
            sessionId: lastChunk.sessionId,
            stream: lastChunk.stream,
            timestamp: lastChunk.timestamp,
            data: trimmedData
        )

        chunks[chunks.count - 1] = trimmedChunk
        totalBytes += trimmedChunk.data.count
        totalLines += estimatedLineCount(in: trimmedChunk.data)
        isTruncated = true
    }

    private mutating func removeFirstChunk() {
        guard let firstChunk = chunks.first else {
            return
        }

        totalBytes -= firstChunk.data.count
        totalLines -= estimatedLineCount(in: firstChunk.data)
        chunks.removeFirst()
    }

    private mutating func normalizedData(from data: Data) -> Data {
        guard data.count > byteLimit else {
            return data
        }

        isTruncated = true
        return Data(data.suffix(byteLimit))
    }

    private func estimatedLineCount(in data: Data) -> Int {
        guard !data.isEmpty else {
            return 0
        }

        let newlineCount = data.reduce(into: 0) { count, byte in
            if byte == 0x0A {
                count += 1
            }
        }

        return max(1, newlineCount)
    }
}
