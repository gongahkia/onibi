import Foundation

public enum RealtimeGatewayCodec {
    private static let newline = Data([0x0A])
    private static let frameLengthByteCount = 4
    private static let encoder: JSONEncoder = {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }()

    public static func encodeFrame<T: Encodable>(_ frame: T) throws -> Data {
        try encodeProxyJSONFrame(frame)
    }

    public static func encodeLegacyNDJSONFrame<T: Encodable>(_ frame: T) throws -> Data {
        var data = try encoder.encode(frame)
        data.append(newline)
        return data
    }

    public static func encodeProxyJSONFrame<T: Encodable>(_ frame: T) throws -> Data {
        var payload = Data([LocalSessionProxyFrameKind.json])
        payload.append(try encoder.encode(frame))
        return lengthPrefixed(payload)
    }

    public static func encodeProxyOutputFrame(_ frame: LocalSessionProxyOutputFrame) throws -> Data {
        let header = try encoder.encode(frame.header)
        var payload = Data([LocalSessionProxyFrameKind.output])
        appendUInt32BE(UInt32(header.count), to: &payload)
        payload.append(header)
        payload.append(frame.data)
        return lengthPrefixed(payload)
    }

    public static func encodeRealtimeOutputBatch(_ frame: RealtimeOutputBatchFrame) throws -> Data {
        let header = try encoder.encode(frame.header)
        var payload = Data([RealtimeBinaryFrameKind.outputBatch])
        appendUInt32BE(UInt32(header.count), to: &payload)
        payload.append(header)
        payload.append(frame.data)
        return payload
    }

    public static func decodeEnvelope(from data: Data) throws -> LocalSessionProxyEnvelope {
        if data.first == LocalSessionProxyFrameKind.json {
            return try JSONDateCodec.decoder.decode(LocalSessionProxyEnvelope.self, from: Data(data.dropFirst()))
        }
        if data.first == LocalSessionProxyFrameKind.output {
            let frame = try decodeProxyOutputFrame(from: data)
            return LocalSessionProxyEnvelope(type: frame.header.type)
        }
        return try JSONDateCodec.decoder.decode(LocalSessionProxyEnvelope.self, from: data)
    }

    public static func decodeProxyJSONFrame<T: Decodable>(_ type: T.Type, from data: Data) throws -> T {
        if data.first == LocalSessionProxyFrameKind.json {
            return try JSONDateCodec.decoder.decode(T.self, from: Data(data.dropFirst()))
        }
        return try JSONDateCodec.decoder.decode(T.self, from: data)
    }

    public static func decodeProxyOutputFrame(from data: Data) throws -> LocalSessionProxyOutputFrame {
        if data.first == LocalSessionProxyFrameKind.output {
            guard data.count >= 1 + frameLengthByteCount else {
                throw RealtimeGatewayCodecError.incompleteBinaryHeader
            }
            let headerLength = Int(readUInt32BE(from: data, offset: 1))
            let headerStart = 1 + frameLengthByteCount
            let bodyStart = headerStart + headerLength
            guard data.count >= bodyStart else {
                throw RealtimeGatewayCodecError.incompleteBinaryHeader
            }
            let header = try JSONDateCodec.decoder.decode(
                LocalSessionProxyOutputHeader.self,
                from: Data(data[headerStart..<bodyStart])
            )
            let body = Data(data[bodyStart..<data.count])
            return LocalSessionProxyOutputFrame(header: header, data: body)
        }

        let jsonData = data.first == LocalSessionProxyFrameKind.json ? Data(data.dropFirst()) : data
        let message = try JSONDateCodec.decoder.decode(LocalSessionProxyOutputMessage.self, from: jsonData)
        guard let decodedData = message.decodedData else {
            throw RealtimeGatewayCodecError.invalidOutputPayload
        }
        return LocalSessionProxyOutputFrame(
            header: LocalSessionProxyOutputHeader(
                sessionId: message.sessionId,
                stream: message.stream,
                timestamp: message.timestamp,
                byteCount: decodedData.count
            ),
            data: decodedData
        )
    }

    public static func decodeRealtimeOutputBatch(from data: Data) throws -> RealtimeOutputBatchFrame {
        guard data.first == RealtimeBinaryFrameKind.outputBatch else {
            throw RealtimeGatewayCodecError.invalidBinaryFrameKind
        }
        guard data.count >= 1 + frameLengthByteCount else {
            throw RealtimeGatewayCodecError.incompleteBinaryHeader
        }
        let headerLength = Int(readUInt32BE(from: data, offset: 1))
        let headerStart = 1 + frameLengthByteCount
        let bodyStart = headerStart + headerLength
        guard data.count >= bodyStart else {
            throw RealtimeGatewayCodecError.incompleteBinaryHeader
        }
        let header = try JSONDateCodec.decoder.decode(
            RealtimeOutputBatchHeader.self,
            from: Data(data[headerStart..<bodyStart])
        )
        return RealtimeOutputBatchFrame(
            header: header,
            data: Data(data[bodyStart..<data.count])
        )
    }

    public static func extractFrames(from buffer: inout Data) -> [Data] {
        var frames: [Data] = []

        while buffer.count >= frameLengthByteCount {
            if buffer.first == 0x7B {
                return extractLegacyFrames(from: &buffer)
            }

            let frameLength = Int(readUInt32BE(from: buffer, offset: 0))
            guard frameLength > 0 else {
                buffer.removeSubrange(0..<frameLengthByteCount)
                continue
            }
            guard buffer.count >= frameLengthByteCount + frameLength else {
                break
            }

            let frameStart = frameLengthByteCount
            let frameEnd = frameStart + frameLength
            frames.append(Data(buffer[frameStart..<frameEnd]))
            buffer.removeSubrange(0..<frameEnd)
        }

        return frames
    }

    private static func extractLegacyFrames(from buffer: inout Data) -> [Data] {
        var frames: [Data] = []

        while let newlineIndex = buffer.firstIndex(of: 0x0A) {
            let frame = buffer.prefix(upTo: newlineIndex)
            frames.append(Data(frame))
            buffer.removeSubrange(...newlineIndex)
        }

        return frames.filter { !$0.isEmpty }
    }

    private static func lengthPrefixed(_ payload: Data) -> Data {
        var data = Data()
        appendUInt32BE(UInt32(payload.count), to: &data)
        data.append(payload)
        return data
    }

    private static func appendUInt32BE(_ value: UInt32, to data: inout Data) {
        data.append(UInt8((value >> 24) & 0xFF))
        data.append(UInt8((value >> 16) & 0xFF))
        data.append(UInt8((value >> 8) & 0xFF))
        data.append(UInt8(value & 0xFF))
    }

    private static func readUInt32BE(from data: Data, offset: Int) -> UInt32 {
        (UInt32(data[offset]) << 24)
            | (UInt32(data[offset + 1]) << 16)
            | (UInt32(data[offset + 2]) << 8)
            | UInt32(data[offset + 3])
    }
}

public enum RealtimeGatewayCodecError: Error {
    case incompleteBinaryHeader
    case invalidBinaryFrameKind
    case invalidOutputPayload
}
