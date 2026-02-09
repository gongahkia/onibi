import XCTest
@testable import Onibi

final class LRUCacheTests: XCTestCase {
    
    // MARK: - Basic Operations
    
    func testSetAndGet() {
        let cache = LRUCache<String, Int>(capacity: 3)
        
        cache.set("a", value: 1)
        cache.set("b", value: 2)
        cache.set("c", value: 3)
        
        XCTAssertEqual(cache.get("a"), 1)
        XCTAssertEqual(cache.get("b"), 2)
        XCTAssertEqual(cache.get("c"), 3)
    }
    
    func testGetNonExistent() {
        let cache = LRUCache<String, Int>(capacity: 3)
        
        XCTAssertNil(cache.get("nonexistent"))
    }
    
    // MARK: - Capacity Tests
    
    func testEvictionAtCapacity() {
        let cache = LRUCache<String, Int>(capacity: 2)
        
        cache.set("a", value: 1)
        cache.set("b", value: 2)
        cache.set("c", value: 3) // Should evict "a"
        
        XCTAssertNil(cache.get("a"))
        XCTAssertEqual(cache.get("b"), 2)
        XCTAssertEqual(cache.get("c"), 3)
    }
    
    func testLRUEvictionOrder() {
        let cache = LRUCache<String, Int>(capacity: 2)
        
        cache.set("a", value: 1)
        cache.set("b", value: 2)
        _ = cache.get("a") // Access "a" to make it recently used
        cache.set("c", value: 3) // Should evict "b" (least recently used)
        
        XCTAssertEqual(cache.get("a"), 1)
        XCTAssertNil(cache.get("b"))
        XCTAssertEqual(cache.get("c"), 3)
    }
    
    // MARK: - Update Tests
    
    func testUpdateExisting() {
        let cache = LRUCache<String, Int>(capacity: 3)
        
        cache.set("a", value: 1)
        cache.set("a", value: 10)
        
        XCTAssertEqual(cache.get("a"), 10)
    }
    
    // MARK: - Clear Tests
    
    func testClear() {
        let cache = LRUCache<String, Int>(capacity: 3)
        
        cache.set("a", value: 1)
        cache.set("b", value: 2)
        cache.clear()
        
        XCTAssertNil(cache.get("a"))
        XCTAssertNil(cache.get("b"))
    }
    
    // MARK: - Thread Safety Tests
    
    func testConcurrentAccess() {
        let cache = LRUCache<Int, Int>(capacity: 100)
        let expectation = XCTestExpectation(description: "Concurrent access")
        expectation.expectedFulfillmentCount = 10
        
        for i in 0..<10 {
            DispatchQueue.global().async {
                for j in 0..<100 {
                    cache.set(i * 100 + j, value: j)
                    _ = cache.get(i * 100 + j)
                }
                expectation.fulfill()
            }
        }
        
        wait(for: [expectation], timeout: 10.0)
        // If we get here without crash, thread safety works
    }
    
    // MARK: - Edge Cases
    
    func testCapacityOne() {
        let cache = LRUCache<String, Int>(capacity: 1)
        
        cache.set("a", value: 1)
        XCTAssertEqual(cache.get("a"), 1)
        
        cache.set("b", value: 2)
        XCTAssertNil(cache.get("a"))
        XCTAssertEqual(cache.get("b"), 2)
    }
}
