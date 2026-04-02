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
        XCTAssertEqual(String(data: snapshot.chunks[0].data, encoding: .utf8), "hello\n")
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
