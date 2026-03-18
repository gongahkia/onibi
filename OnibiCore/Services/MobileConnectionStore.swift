import Foundation

public final class MobileConnectionStore: @unchecked Sendable {
    private let defaults: UserDefaults
    private let configurationKey: String
    private let tokenStore: PairingTokenStore

    public init(
        defaults: UserDefaults = .standard,
        configurationKey: String = "com.onibi.mobile.connection",
        tokenStore: PairingTokenStore = PairingTokenStore(service: "com.onibi.mobile.phone", account: "pairing-token")
    ) {
        self.defaults = defaults
        self.configurationKey = configurationKey
        self.tokenStore = tokenStore
    }

    public func loadConfiguration() -> (configuration: MobileConnectionConfiguration, token: String)? {
        guard
            let data = defaults.data(forKey: configurationKey),
            let configuration = try? JSONDecoder().decode(MobileConnectionConfiguration.self, from: data),
            let token = tokenStore.loadToken(),
            !token.isEmpty
        else {
            return nil
        }

        return (configuration, token)
    }

    public func saveConfiguration(baseURLString: String, token: String) throws {
        let configuration = MobileConnectionConfiguration(baseURLString: baseURLString)
        let data = try JSONEncoder().encode(configuration)
        defaults.set(data, forKey: configurationKey)
        try tokenStore.saveToken(token)
    }

    public func clear() {
        defaults.removeObject(forKey: configurationKey)
        tokenStore.clear()
    }
}
