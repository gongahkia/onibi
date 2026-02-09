import Foundation

/// Dependency injection container for service management
final class DependencyContainer {
    static let shared = DependencyContainer()
    
    private var services: [String: Any] = [:]
    private let lock = NSLock()
    
    private init() {
        registerDefaultServices()
    }
    
    /// Register default services
    private func registerDefaultServices() {
        // Register EventBus
        register(EventBus.self) { EventBus.shared }
    }
    
    /// Register a service factory
    func register<T>(_ type: T.Type, factory: @escaping () -> T) {
        let key = String(describing: type)
        lock.lock()
        defer { lock.unlock() }
        services[key] = factory
    }
    
    /// Register a singleton instance
    func register<T>(_ type: T.Type, instance: T) {
        let key = String(describing: type)
        lock.lock()
        defer { lock.unlock() }
        services[key] = instance
    }
    
    /// Resolve a service
    func resolve<T>(_ type: T.Type) -> T? {
        let key = String(describing: type)
        lock.lock()
        defer { lock.unlock() }
        
        if let instance = services[key] as? T {
            return instance
        }
        
        if let factory = services[key] as? () -> T {
            let instance = factory()
            services[key] = instance
            return instance
        }
        
        return nil
    }
    
    enum ServiceError: Error {
        case serviceNotFound(String)
    }

    /// Resolve a service or throw if not found
    func resolveRequired<T>(_ type: T.Type) throws -> T {
        guard let service = resolve(type) else {
            throw ServiceError.serviceNotFound(String(describing: type))
        }
        return service
    }
}

/// Property wrapper for dependency injection
@propertyWrapper
struct Injected<T> {
    private var service: T?
    
    var wrappedValue: T {
        mutating get {
            if service == nil {
                service = DependencyContainer.shared.resolve(T.self)
            }
            guard let service = service else {
                fatalError("Service \(T.self) not registered")
            }
            return service
        }
    }
    
    init() {}
}
