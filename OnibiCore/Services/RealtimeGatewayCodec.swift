import Foundation

public enum RealtimeGatewayCodec {
    private static let newline = Data([0x0A])
    private static let encoder: JSONEncoder = {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }()

    public static func encodeFrame<T: Encodable>(_ frame: T) throws -> Data {
        var data = try encoder.encode(frame)
        data.append(newline)
        return data
    }

    public static func decodeEnvelope(from data: Data) throws -> LocalSessionProxyEnvelope {
        try JSONDateCodec.decoder.decode(LocalSessionProxyEnvelope.self, from: data)
    }

    public static func extractFrames(from buffer: inout Data) -> [Data] {
        var frames: [Data] = []

        while let newlineIndex = buffer.firstIndex(of: 0x0A) {
            let frame = buffer.prefix(upTo: newlineIndex)
            frames.append(Data(frame))
            buffer.removeSubrange(...newlineIndex)
        }

        return frames.filter { !$0.isEmpty }
    }
}
