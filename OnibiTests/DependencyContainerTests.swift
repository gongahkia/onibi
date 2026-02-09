import XCTest
@testable import Onibi
final class DependencyContainerTests: XCTestCase {
    func testRegisterAndResolveInstance() {
        let container = DependencyContainer.shared
        let bus = EventBus.shared
        container.register(EventBus.self, instance: bus)
        let resolved = container.resolve(EventBus.self)
        XCTAssertNotNil(resolved)
        XCTAssertTrue(resolved === bus)
    }
    func testResolveUnregisteredReturnsNil() {
        let container = DependencyContainer.shared
        struct Unregistered {}
        let result = container.resolve(Unregistered.self)
        XCTAssertNil(result)
    }
    func testResolveRequiredThrowsForMissing() {
        let container = DependencyContainer.shared
        struct Missing {}
        XCTAssertThrowsError(try container.resolveRequired(Missing.self)) { error in
            guard case DependencyContainer.ServiceError.serviceNotFound = error else {
                XCTFail("expected serviceNotFound, got \(error)")
                return
            }
        }
    }
    func testRegisterFactory() {
        let container = DependencyContainer.shared
        container.register(EventBus.self) { EventBus.shared }
        let resolved = container.resolve(EventBus.self)
        XCTAssertNotNil(resolved)
    }
    func testServiceErrorDescriptions() {
        let notFound = DependencyContainer.ServiceError.serviceNotFound("Foo")
        XCTAssertTrue(notFound.description.contains("Foo"))
        let circular = DependencyContainer.ServiceError.circularDependency("A -> B -> A")
        XCTAssertTrue(circular.description.contains("A -> B -> A"))
    }
}
