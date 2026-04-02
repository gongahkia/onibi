import Foundation
import OnibiCore

enum RemoteInputByteTranslator {
    static func data(for payload: RemoteInputPayload) throws -> Data {
        guard payload.isValid else {
            throw RemoteControlError.invalidInputPayload
        }

        switch payload.kind {
        case .text:
            guard let text = payload.text else {
                throw RemoteControlError.invalidInputPayload
            }
            return Data(text.utf8)
        case .key:
            guard let key = payload.key else {
                throw RemoteControlError.invalidInputPayload
            }
            return try data(for: key)
        }
    }

    static func data(for key: RemoteInputKey) throws -> Data {
        switch key {
        case .enter:
            return Data([0x0D])
        case .ctrlC:
            return Data([0x03])
        case .arrowUp:
            return Data([0x1B, 0x5B, 0x41])
        case .arrowDown:
            return Data([0x1B, 0x5B, 0x42])
        case .arrowRight:
            return Data([0x1B, 0x5B, 0x43])
        case .arrowLeft:
            return Data([0x1B, 0x5B, 0x44])
        case .space:
            return Data([0x20])
        }
    }
}
