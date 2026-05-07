import XCTest
import OnibiCore
@testable import Onibi

final class SessionOutputBufferTests: XCTestCase {
    func testAppendTracksCursorAndPayload() {
        var buffer = SessionOutputBuffer(lineLimit: 10, byteLimit: 1024)
        let session = makeSession()

        buffer.append(
            sessionId: session.id,
            stream: .stdout,
            data: Data("hello\n".utf8),
            timestamp: Date()
        )

        let snapshot = buffer.snapshot(for: session)
        XCTAssertEqual(snapshot.chunks.count, 1)
        XCTAssertEqual(snapshot.bufferCursor, snapshot.chunks.first?.id)
        XCTAssertEqual(snapshot.startCursor, snapshot.chunks.first?.id)
        XCTAssertEqual(snapshot.endCursor, snapshot.chunks.first?.id)
        XCTAssertEqual(String(data: snapshot.chunks[0].data, encoding: .utf8), "hello\n")
    }

    func testSnapshotCanReturnChunksAfterCursorWithLimit() {
        var buffer = SessionOutputBuffer(lineLimit: 10, byteLimit: 1024)
        let session = makeSession()

        let first = buffer.append(sessionId: session.id, stream: .stdout, data: Data("one\n".utf8)).chunk
        _ = buffer.append(sessionId: session.id, stream: .stdout, data: Data("two\n".utf8))
        let third = buffer.append(sessionId: session.id, stream: .stdout, data: Data("three\n".utf8)).chunk

        let snapshot = buffer.snapshot(for: session, after: first.id, limit: 1)

        XCTAssertEqual(snapshot.chunks.count, 1)
        XCTAssertEqual(String(data: snapshot.chunks[0].data, encoding: .utf8), "three\n")
        XCTAssertEqual(snapshot.bufferCursor, third.id)
        XCTAssertEqual(snapshot.startCursor, third.id)
        XCTAssertEqual(snapshot.endCursor, third.id)
    }

    func testBufferDropsOldestChunksWhenByteLimitIsExceeded() {
        var buffer = SessionOutputBuffer(lineLimit: 10, byteLimit: 12)
        let session = makeSession()

        buffer.append(sessionId: session.id, stream: .stdout, data: Data("12345".utf8))
        buffer.append(sessionId: session.id, stream: .stdout, data: Data("67890".utf8))
        buffer.append(sessionId: session.id, stream: .stdout, data: Data("abcde".utf8))

        let snapshot = buffer.snapshot(for: session)
        XCTAssertEqual(snapshot.chunks.count, 2)
        XCTAssertEqual(String(data: snapshot.chunks[0].data, encoding: .utf8), "67890")
        XCTAssertEqual(String(data: snapshot.chunks[1].data, encoding: .utf8), "abcde")
        XCTAssertTrue(snapshot.truncated)
    }

    func testAppendReportsTruncationEventCount() {
        var buffer = SessionOutputBuffer(lineLimit: 10, byteLimit: 5)
        let result = buffer.append(
            sessionId: "session-1",
            stream: .stdout,
            data: Data("1234567890".utf8)
        )

        XCTAssertEqual(result.truncationEventCount, 1)
        XCTAssertEqual(String(data: result.chunk.data, encoding: .utf8), "67890")
    }

    func testBufferDropsOldestChunksWhenLineLimitIsExceeded() {
        var buffer = SessionOutputBuffer(lineLimit: 2, byteLimit: 1024)
        let session = makeSession()

        buffer.append(sessionId: session.id, stream: .stdout, data: Data("one\n".utf8))
        buffer.append(sessionId: session.id, stream: .stdout, data: Data("two\n".utf8))
        buffer.append(sessionId: session.id, stream: .stdout, data: Data("three\n".utf8))

        let snapshot = buffer.snapshot(for: session)
        XCTAssertEqual(snapshot.chunks.count, 2)
        XCTAssertEqual(String(data: snapshot.chunks[0].data, encoding: .utf8), "two\n")
        XCTAssertEqual(String(data: snapshot.chunks[1].data, encoding: .utf8), "three\n")
        XCTAssertTrue(snapshot.truncated)
    }

    func testAppendingLargeOutputAvoidsQuadraticTrimBehavior() {
        var buffer = SessionOutputBuffer(lineLimit: 10_000, byteLimit: 1 * 1024 * 1024)
        let session = makeSession()
        let chunk = Data(repeating: 0x41, count: 100 * 1024)

        for _ in 0..<1_024 {
            buffer.append(
                sessionId: session.id,
                stream: .stdout,
                data: chunk
            )
        }
        let snapshot = buffer.snapshot(for: session)
        XCTAssertLessThanOrEqual(snapshot.chunks.reduce(0) { $0 + $1.data.count }, 1 * 1024 * 1024)
        XCTAssertTrue(snapshot.truncated)
        XCTAssertGreaterThan(snapshot.droppedByteCount, 90 * 1024 * 1024)
    }

    func testSnapshotReportsCursorMetadataAfterTruncation() {
        var buffer = SessionOutputBuffer(lineLimit: 10, byteLimit: 12)
        let session = makeSession()

        _ = buffer.append(sessionId: session.id, stream: .stdout, data: Data("12345".utf8))
        let second = buffer.append(sessionId: session.id, stream: .stdout, data: Data("67890".utf8)).chunk
        let third = buffer.append(sessionId: session.id, stream: .stdout, data: Data("abcde".utf8)).chunk

        let snapshot = buffer.snapshot(for: session)
        XCTAssertEqual(snapshot.oldestCursor, second.id)
        XCTAssertEqual(snapshot.newestCursor, third.id)
        XCTAssertEqual(snapshot.droppedChunkCount, 1)
        XCTAssertEqual(snapshot.droppedByteCount, 5)
        XCTAssertEqual(snapshot.bufferCursor, third.id)
    }

    private func makeSession() -> ControllableSessionSnapshot {
        ControllableSessionSnapshot(
            id: "session-1",
            displayName: "session-1",
            startedAt: Date(),
            lastActivityAt: Date(),
            status: .running,
            isControllable: true,
            workingDirectory: "/tmp",
            lastCommandPreview: nil,
            bufferCursor: nil
        )
    }
}
