import Foundation
import Security

public final class PairingTokenStore: @unchecked Sendable {
    private let service: String
    private let account: String

    public init(service: String = "com.onibi.mobile", account: String = "pairing-token") {
        self.service = service
        self.account = account
    }

    public func loadToken() -> String? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne
        ]

        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)
        guard
            status == errSecSuccess,
            let data = item as? Data,
            let token = String(data: data, encoding: .utf8)
        else {
            return nil
        }

        return token
    }

    @discardableResult
    public func ensureToken() throws -> String {
        if let existing = loadToken(), !existing.isEmpty {
            return existing
        }
        return try rotateToken()
    }

    @discardableResult
    public func rotateToken() throws -> String {
        let token = Self.generateToken()
        try saveToken(token)
        return token
    }

    public func saveToken(_ token: String) throws {
        let data = Data(token.utf8)
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account
        ]

        let updateAttributes: [String: Any] = [
            kSecValueData as String: data
        ]

        let updateStatus = SecItemUpdate(query as CFDictionary, updateAttributes as CFDictionary)
        if updateStatus == errSecSuccess {
            return
        }

        let createAttributes: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecValueData as String: data
        ]

        let createStatus = SecItemAdd(createAttributes as CFDictionary, nil)
        guard createStatus == errSecSuccess else {
            throw PairingTokenStoreError.keychainFailure(status: createStatus)
        }
    }

    public func clear() {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account
        ]

        SecItemDelete(query as CFDictionary)
    }

    private static func generateToken() -> String {
        var bytes = [UInt8](repeating: 0, count: 32)
        _ = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        let data = Data(bytes)
        return data.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }
}

public enum PairingTokenStoreError: LocalizedError {
    case keychainFailure(status: OSStatus)

    public var errorDescription: String? {
        switch self {
        case .keychainFailure(let status):
            return "Keychain operation failed with status \(status)"
        }
    }
}
