import Foundation

/// Pure helper so we can unit-test the rotation-reminder threshold logic
/// without touching the singleton.
enum TokenAgeCalculator {
    static func ageInDays(from issuedAt: Date?, now: Date = Date()) -> Int? {
        guard let issuedAt else { return nil }
        return Calendar.current.dateComponents([.day], from: issuedAt, to: now).day
    }

    static func shouldRemindToRotate(ageDays: Int?, threshold: Int) -> Bool {
        guard let ageDays else { return false }
        return ageDays >= threshold
    }
}
