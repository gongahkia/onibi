import Foundation
import OnibiCore

struct SessionOutputAppendResult {
    let chunk: SessionOutputChunk
    let truncationEventCount: Int
}

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
        _ = trimIfNeeded()
    }

    @discardableResult
    mutating func append(
        sessionId: String,
        stream: SessionOutputStream,
        data: Data,
        timestamp: Date = Date()
    ) -> SessionOutputAppendResult {
        var truncationEventCount = 0
        let normalizedData = normalizedData(
            from: data,
            truncationEventCount: &truncationEventCount
        )
        let chunk = SessionOutputChunk(
            sessionId: sessionId,
            stream: stream,
            timestamp: timestamp,
            data: normalizedData
        )
        chunks.append(chunk)
        totalBytes += chunk.data.count
        totalLines += estimatedLineCount(in: chunk.data)
        truncationEventCount += trimIfNeeded()
        return SessionOutputAppendResult(
            chunk: chunk,
            truncationEventCount: truncationEventCount
        )
    }

    func snapshot(
        for session: ControllableSessionSnapshot,
        after cursor: String? = nil,
        limit: Int? = nil
    ) -> SessionOutputBufferSnapshot {
        let requestedChunks = chunksAfter(cursor: cursor, limit: limit)
        return SessionOutputBufferSnapshot(
            session: session,
            bufferCursor: currentCursor,
            startCursor: requestedChunks.first?.id,
            endCursor: requestedChunks.last?.id,
            chunks: requestedChunks,
            truncated: isTruncated
        )
    }

    private func chunksAfter(cursor: String?, limit: Int?) -> [SessionOutputChunk] {
        let boundedLimit = limit.map { max(1, min($0, chunks.count)) }
        let candidateChunks: [SessionOutputChunk]
        if let cursor, let cursorIndex = chunks.firstIndex(where: { $0.id == cursor }) {
            candidateChunks = Array(chunks.dropFirst(cursorIndex + 1))
        } else {
            candidateChunks = chunks
        }

        guard let boundedLimit, candidateChunks.count > boundedLimit else {
            return candidateChunks
        }
        return Array(candidateChunks.suffix(boundedLimit))
    }

    private mutating func trimIfNeeded() -> Int {
        var truncationEventCount = 0
        while chunks.count > 1 && (totalBytes > byteLimit || totalLines > lineLimit) {
            removeFirstChunk()
            isTruncated = true
            truncationEventCount += 1
        }

        guard
            let lastChunk = chunks.last,
            totalBytes > byteLimit
        else {
            return truncationEventCount
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
        truncationEventCount += 1
        return truncationEventCount
    }

    private mutating func removeFirstChunk() {
        guard let firstChunk = chunks.first else {
            return
        }

        totalBytes -= firstChunk.data.count
        totalLines -= estimatedLineCount(in: firstChunk.data)
        chunks.removeFirst()
    }

    private mutating func normalizedData(
        from data: Data,
        truncationEventCount: inout Int
    ) -> Data {
        guard data.count > byteLimit else {
            return data
        }

        isTruncated = true
        truncationEventCount += 1
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
