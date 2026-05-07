import Foundation
import OnibiCore

struct SessionOutputAppendResult {
    let chunk: SessionOutputChunk
    let truncationEventCount: Int
}

struct SessionOutputBuffer {
    private(set) var lineLimit: Int
    private(set) var byteLimit: Int
    private var storage: [SessionOutputChunk?] = []
    private var headIndex = 0
    private(set) var isTruncated = false
    private(set) var droppedChunkCount = 0
    private(set) var droppedByteCount = 0
    private(set) var truncationEventCount = 0

    private var totalBytes = 0
    private var totalLines = 0

    init(lineLimit: Int, byteLimit: Int) {
        self.lineLimit = max(1, lineLimit)
        self.byteLimit = max(1, byteLimit)
    }

    var chunks: [SessionOutputChunk] {
        guard headIndex < storage.count else {
            return []
        }
        return Array(storage[headIndex...].compactMap { $0 })
    }

    var currentCursor: String? {
        storage.last??.id
    }

    var oldestCursor: String? {
        storage.indices.lazy
            .filter { $0 >= headIndex }
            .compactMap { storage[$0]?.id }
            .first
    }

    mutating func reconfigure(lineLimit: Int, byteLimit: Int) {
        self.lineLimit = max(1, lineLimit)
        self.byteLimit = max(1, byteLimit)
        _ = trimIfNeeded()
        compactIfNeeded(force: true)
    }

    @discardableResult
    mutating func append(
        sessionId: String,
        stream: SessionOutputStream,
        data: Data,
        timestamp: Date = Date()
    ) -> SessionOutputAppendResult {
        var eventCount = 0
        let normalizedData = normalizedData(from: data, truncationEventCount: &eventCount)
        let chunk = SessionOutputChunk(
            sessionId: sessionId,
            stream: stream,
            timestamp: timestamp,
            data: normalizedData
        )
        storage.append(chunk)
        totalBytes += chunk.data.count
        totalLines += estimatedLineCount(in: chunk.data)
        eventCount += trimIfNeeded()
        truncationEventCount += eventCount
        compactIfNeeded()
        return SessionOutputAppendResult(chunk: chunk, truncationEventCount: eventCount)
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
            truncated: isTruncated,
            droppedChunkCount: droppedChunkCount,
            droppedByteCount: droppedByteCount,
            oldestCursor: oldestCursor,
            newestCursor: currentCursor,
            truncationEventCount: truncationEventCount
        )
    }

    private func chunksAfter(cursor: String?, limit: Int?) -> [SessionOutputChunk] {
        var startIndex = headIndex
        if let cursor {
            for index in headIndex..<storage.count where storage[index]?.id == cursor {
                startIndex = index + 1
                break
            }
        }

        var result: [SessionOutputChunk] = []
        result.reserveCapacity(storage.count - startIndex)
        for index in startIndex..<storage.count {
            if let chunk = storage[index] {
                result.append(chunk)
            }
        }

        guard let limit else {
            return result
        }
        let boundedLimit = max(1, min(limit, result.count))
        guard result.count > boundedLimit else {
            return result
        }
        return Array(result.suffix(boundedLimit))
    }

    private mutating func trimIfNeeded() -> Int {
        var eventCount = 0
        while liveChunkCount > 1 && (totalBytes > byteLimit || totalLines > lineLimit) {
            removeOldestChunk()
            isTruncated = true
            eventCount += 1
        }

        guard
            let lastIndex = storage.indices.last,
            let lastChunk = storage[lastIndex],
            totalBytes > byteLimit
        else {
            return eventCount
        }

        let trimmedData = Data(lastChunk.data.suffix(byteLimit))
        totalBytes -= lastChunk.data.count
        totalLines -= estimatedLineCount(in: lastChunk.data)
        droppedByteCount += max(0, lastChunk.data.count - trimmedData.count)

        let trimmedChunk = SessionOutputChunk(
            id: lastChunk.id,
            sessionId: lastChunk.sessionId,
            stream: lastChunk.stream,
            timestamp: lastChunk.timestamp,
            data: trimmedData
        )

        storage[lastIndex] = trimmedChunk
        totalBytes += trimmedChunk.data.count
        totalLines += estimatedLineCount(in: trimmedChunk.data)
        isTruncated = true
        return eventCount + 1
    }

    private mutating func removeOldestChunk() {
        while headIndex < storage.count {
            guard let chunk = storage[headIndex] else {
                headIndex += 1
                continue
            }
            totalBytes -= chunk.data.count
            totalLines -= estimatedLineCount(in: chunk.data)
            droppedChunkCount += 1
            droppedByteCount += chunk.data.count
            storage[headIndex] = nil
            headIndex += 1
            return
        }
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
        droppedByteCount += data.count - byteLimit
        return Data(data.suffix(byteLimit))
    }

    private var liveChunkCount: Int {
        storage.count - headIndex
    }

    private mutating func compactIfNeeded(force: Bool = false) {
        guard force || headIndex > 1024 && headIndex * 2 > storage.count else {
            return
        }
        storage.removeFirst(headIndex)
        headIndex = 0
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
