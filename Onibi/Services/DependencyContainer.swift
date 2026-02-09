import Foundation

/// Dependency injection container for service management
final class DependencyContainer {
    static let shared = DependencyContainer()
    
    private var services: [String: Any] = [:]
    private let lock = NSLock()
    private var resolvingStack: [String] = []
    
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
    
    enum ServiceError: Error, CustomStringConvertible {
        case serviceNotFound(String)
        case circularDependency(String)
        var description: String {
            switch self {
            case .serviceNotFound(let type): return "service not found: \(type)"
            case .circularDependency(let cycle): return "circular dependency: \(cycle)"
            }
        }
    }
    /// Resolve a service with cycle detection
    func resolve<T>(_ type: T.Type) -> T? {
        return try? resolveRequired(type)
    }
    /// Resolve a service or throw if not found
    func resolveRequired<T>(_ type: T.Type) throws -> T {
        let key = String(describing: type)
        lock.lock()
        defer { lock.unlock() }
        if resolvingStack.contains(key) {
            let cycle = (resolvingStack + [key]).joined(separator: " -> ")
            throw ServiceError.circularDependency(cycle)
        }
        if let instance = services[key] as? T {
            return instance
        }
        if let factory = services[key] as? () -> T {
            resolvingStack.append(key)
            let instance = factory()
            resolvingStack.removeLast()
            services[key] = instance
            return instance
        }
        throw ServiceError.serviceNotFound(key)
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
            guard let resolved = service else {
                ErrorReporter.shared.report(
                    title: "DependencyContainer",
                    message: "Service \(T.self) not registered",
                    severity: .critical
                )
                fatalError("Service \(T.self) not registered") // unrecoverable: DI misconfiguration
            }
            return resolved
        }
    }
    init() {}
}
