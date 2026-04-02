import XCTest
@testable import OnibiCore

final class RealtimeGatewayCodecTests: XCTestCase {
    func testEncodeFrameProducesNDJSONLine() throws {
        let frame = LocalSessionProxyHeartbeatMessage(
            sessionId: "session-1",
            timestamp: Date(timeIntervalSince1970: 0)
        )

        let data = try RealtimeGatewayCodec.encodeFrame(frame)
        XCTAssertEqual(data.last, 0x0A)
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
