import XCTest
@testable import OnibiCore

final class RealtimeGatewayCodecTests: XCTestCase {
    func testEncodeFrameProducesLengthPrefixedV2Frame() throws {
        let frame = LocalSessionProxyHeartbeatMessage(
            sessionId: "session-1",
            timestamp: Date(timeIntervalSince1970: 0)
        )

        let data = try RealtimeGatewayCodec.encodeFrame(frame)
        XCTAssertGreaterThan(data.count, 5)
        XCTAssertEqual(data[4], LocalSessionProxyFrameKind.json)
    }

    func testExtractFramesKeepsTrailingPartialFrameBuffered() throws {
        let frame1 = try RealtimeGatewayCodec.encodeFrame(
            LocalSessionProxyStateMessage(sessionId: "session-1", status: .running)
        )
        let frame2 = try RealtimeGatewayCodec.encodeFrame(
            LocalSessionProxyHeartbeatMessage(
                sessionId: "session-1",
                timestamp: Date(timeIntervalSince1970: 1)
            )
        )

        var buffer = Data()
        buffer.append(frame1)
        buffer.append(frame2.dropLast())

        let frames = RealtimeGatewayCodec.extractFrames(from: &buffer)
        XCTAssertEqual(frames.count, 1)
        XCTAssertFalse(buffer.isEmpty)

        let envelope = try RealtimeGatewayCodec.decodeEnvelope(from: frames[0])
        XCTAssertEqual(envelope.type, .state)
    }

    func testProxyOutputFrameCarriesRawBytes() throws {
        let frame = LocalSessionProxyOutputFrame(
            header: LocalSessionProxyOutputHeader(
                sessionId: "session-1",
                stream: .stdout,
                timestamp: Date(timeIntervalSince1970: 0),
                byteCount: 4
            ),
            data: Data([0x00, 0x01, 0x02, 0xFF])
        )

        var buffer = try RealtimeGatewayCodec.encodeProxyOutputFrame(frame)
        let frames = RealtimeGatewayCodec.extractFrames(from: &buffer)
        XCTAssertEqual(frames.count, 1)

        let decoded = try RealtimeGatewayCodec.decodeProxyOutputFrame(from: frames[0])
        XCTAssertEqual(decoded.header.sessionId, "session-1")
        XCTAssertEqual(decoded.data, Data([0x00, 0x01, 0x02, 0xFF]))
    }

    func testRealtimeOutputBatchRoundTripsBinaryPayload() throws {
        let chunk = SessionOutputChunk(
            id: "cursor-1",
            sessionId: "session-1",
            stream: .stdout,
            timestamp: Date(timeIntervalSince1970: 0),
            data: Data([0x00, 0x01, 0x02, 0xFF])
        )

        let encoded = try RealtimeGatewayCodec.encodeRealtimeOutputBatch(
            RealtimeOutputBatchFrame(chunk: chunk)
        )
        let decoded = try RealtimeGatewayCodec.decodeRealtimeOutputBatch(from: encoded)
        XCTAssertEqual(decoded.header.type, "output_batch")
        XCTAssertEqual(decoded.header.startCursor, "cursor-1")
        XCTAssertEqual(decoded.data, chunk.data)
    }

    func testOutputMessageRoundTripsBase64Payload() throws {
        let message = LocalSessionProxyOutputMessage(
            sessionId: "session-1",
            stream: .stdout,
            timestamp: Date(),
            outputData: Data([0x00, 0x01, 0x02, 0xFF])
        )

        let encoded = try JSONDateCodec.encoder.encode(message)
        let decoded = try JSONDateCodec.decoder.decode(LocalSessionProxyOutputMessage.self, from: encoded)
        XCTAssertEqual(decoded.decodedData, Data([0x00, 0x01, 0x02, 0xFF]))
    }
}
