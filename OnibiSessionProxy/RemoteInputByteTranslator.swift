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
        case .paste:
            guard let text = payload.text else {
                throw RemoteControlError.invalidInputPayload
            }
            return bracketedPaste(Data(text.utf8))
        case .bytes:
            guard let encoded = payload.data, let data = Data(base64Encoded: encoded) else {
                throw RemoteControlError.invalidInputPayload
            }
            return data
        case .file:
            guard let encoded = payload.data, let data = Data(base64Encoded: encoded) else {
                throw RemoteControlError.invalidInputPayload
            }
            return bracketedPaste(data)
        }
    }

    private static func bracketedPaste(_ payload: Data) -> Data {
        var data = Data([0x1B, 0x5B, 0x32, 0x30, 0x30, 0x7E])
        data.append(payload)
        data.append(contentsOf: [0x1B, 0x5B, 0x32, 0x30, 0x31, 0x7E])
        return data
    }

    static func data(for key: RemoteInputKey) throws -> Data {
        switch key {
        case .enter:
            return Data([0x0D])
        case .ctrlC:
            return Data([0x03])
        case .ctrlD:
            return Data([0x04])
        case .ctrlS:
            return Data([0x13])
        case .ctrlQ:
            return Data([0x11])
        case .tab:
            return Data([0x09])
        case .backspace:
            return Data([0x7F])
        case .escape:
            return Data([0x1B])
        case .delete:
            return Data([0x1B, 0x5B, 0x33, 0x7E])
        case .home:
            return Data([0x1B, 0x5B, 0x48])
        case .end:
            return Data([0x1B, 0x5B, 0x46])
        case .pageUp:
            return Data([0x1B, 0x5B, 0x35, 0x7E])
        case .pageDown:
            return Data([0x1B, 0x5B, 0x36, 0x7E])
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
