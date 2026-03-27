import SwiftUI
import OnibiCore

enum PhonePalette {
    static let ember = Color(red: 0.84, green: 0.33, blue: 0.16)
    static let sunrise = Color(red: 0.97, green: 0.64, blue: 0.31)
    static let charcoal = Color(red: 0.14, green: 0.11, blue: 0.10)
    static let smoke = Color(red: 0.40, green: 0.35, blue: 0.33)
    static let fog = Color(red: 0.96, green: 0.94, blue: 0.91)
    static let moss = Color(red: 0.28, green: 0.50, blue: 0.35)
    static let cobalt = Color(red: 0.23, green: 0.42, blue: 0.77)
    static let rose = Color(red: 0.72, green: 0.23, blue: 0.18)
}

struct PhoneBackground: View {
    var body: some View {
        ZStack {
            LinearGradient(
                colors: [
                    PhonePalette.fog,
                    Color.white,
                    PhonePalette.fog.opacity(0.92)
                ],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            )

            Circle()
                .fill(PhonePalette.sunrise.opacity(0.34))
                .frame(width: 280, height: 280)
                .blur(radius: 40)
                .offset(x: 140, y: -260)

            Circle()
                .fill(PhonePalette.ember.opacity(0.18))
                .frame(width: 220, height: 220)
                .blur(radius: 36)
                .offset(x: -150, y: 260)

            Circle()
                .fill(PhonePalette.cobalt.opacity(0.12))
                .frame(width: 180, height: 180)
                .blur(radius: 32)
                .offset(x: 120, y: 380)
        }
        .ignoresSafeArea()
    }
}

struct PhoneCard<Content: View>: View {
    private let content: Content

    init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    var body: some View {
        content
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                RoundedRectangle(cornerRadius: 28, style: .continuous)
                    .fill(Color.white.opacity(0.74))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 28, style: .continuous)
                    .stroke(Color.white.opacity(0.75), lineWidth: 1)
            )
            .shadow(color: Color.black.opacity(0.05), radius: 24, x: 0, y: 18)
    }
}

struct PhoneSectionHeader: View {
    let title: String
    let subtitle: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title)
                .font(.title3.weight(.semibold))
                .foregroundStyle(PhonePalette.charcoal)

            Text(subtitle)
                .font(.subheadline)
                .foregroundStyle(PhonePalette.smoke)
        }
    }
}

struct PhoneMetricTile: View {
    let title: String
    let value: String
    let symbolName: String
    let tint: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Image(systemName: symbolName)
                .font(.headline.weight(.semibold))
                .foregroundStyle(tint)
                .frame(width: 36, height: 36)
                .background(tint.opacity(0.14), in: RoundedRectangle(cornerRadius: 14, style: .continuous))

            VStack(alignment: .leading, spacing: 4) {
                Text(value)
                    .font(.title2.weight(.bold))
                    .foregroundStyle(PhonePalette.charcoal)

                Text(title)
                    .font(.footnote.weight(.medium))
                    .foregroundStyle(PhonePalette.smoke)
            }
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 22, style: .continuous)
                .fill(Color.white.opacity(0.58))
        )
    }
}

struct PhoneBadge: View {
    let title: String
    let symbolName: String
    let tint: Color

    var body: some View {
        Label(title, systemImage: symbolName)
            .font(.caption.weight(.semibold))
            .foregroundStyle(tint)
            .padding(.horizontal, 10)
            .padding(.vertical, 7)
            .background(tint.opacity(0.12), in: Capsule())
    }
}

struct PhoneEmptyStateCard<Accessory: View>: View {
    let title: String
    let message: String
    let symbolName: String
    private let accessory: Accessory

    init(
        title: String,
        message: String,
        symbolName: String,
        @ViewBuilder accessory: () -> Accessory = { EmptyView() }
    ) {
        self.title = title
        self.message = message
        self.symbolName = symbolName
        self.accessory = accessory()
    }

    var body: some View {
        PhoneCard {
            VStack(alignment: .leading, spacing: 16) {
                Image(systemName: symbolName)
                    .font(.system(size: 28, weight: .semibold))
                    .foregroundStyle(PhonePalette.ember)

                VStack(alignment: .leading, spacing: 6) {
                    Text(title)
                        .font(.headline)
                        .foregroundStyle(PhonePalette.charcoal)

                    Text(message)
                        .font(.subheadline)
                        .foregroundStyle(PhonePalette.smoke)
                }

                accessory
            }
        }
    }
}

struct PhonePrimaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        PhonePrimaryButton(configuration: configuration)
    }

    private struct PhonePrimaryButton: View {
        @Environment(\.isEnabled) private var isEnabled
        let configuration: Configuration

        var body: some View {
            configuration.label
                .font(.headline.weight(.semibold))
                .foregroundStyle(.white)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 16)
                .background(
                    LinearGradient(
                        colors: [PhonePalette.ember, PhonePalette.sunrise],
                        startPoint: .leading,
                        endPoint: .trailing
                    ),
                    in: RoundedRectangle(cornerRadius: 18, style: .continuous)
                )
                .opacity(isEnabled ? 1 : 0.5)
                .scaleEffect(configuration.isPressed ? 0.98 : 1)
                .animation(.easeOut(duration: 0.18), value: configuration.isPressed)
        }
    }
}

struct PhoneSecondaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.headline.weight(.semibold))
            .foregroundStyle(PhonePalette.charcoal)
            .frame(maxWidth: .infinity)
            .padding(.vertical, 16)
            .background(Color.white.opacity(0.58), in: RoundedRectangle(cornerRadius: 18, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: 18, style: .continuous)
                    .stroke(Color.white.opacity(0.75), lineWidth: 1)
            )
            .scaleEffect(configuration.isPressed ? 0.98 : 1)
            .animation(.easeOut(duration: 0.18), value: configuration.isPressed)
    }
}

enum PhoneFormats {
    static let relativeDateFormatter: RelativeDateTimeFormatter = {
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .short
        return formatter
    }()

    static func relativeString(for date: Date?) -> String {
        guard let date else {
            return "Waiting"
        }

        return relativeDateFormatter.localizedString(for: date, relativeTo: Date())
    }

    static func dateTimeString(for date: Date?) -> String {
        guard let date else {
            return "Unavailable"
        }

        return date.formatted(date: .abbreviated, time: .shortened)
    }

    static func timeString(for date: Date?) -> String {
        guard let date else {
            return "Unavailable"
        }

        return date.formatted(date: .omitted, time: .shortened)
    }

    static func durationString(for duration: TimeInterval?) -> String? {
        guard let duration else {
            return nil
        }

        if duration < 1 {
            return String(format: "%.0f ms", duration * 1000)
        }

        return String(format: "%.1f s", duration)
    }

    static func hostLabel(from baseURLString: String?) -> String? {
        guard
            let baseURLString,
            let url = URL(string: baseURLString)
        else {
            return nil
        }

        return url.host ?? baseURLString
    }
}

extension AssistantKind {
    var tintColor: Color {
        switch self {
        case .claudeCode:
            return PhonePalette.ember
        case .codex:
            return PhonePalette.cobalt
        case .gemini:
            return .mint
        case .copilot:
            return .indigo
        case .otherAI:
            return .teal
        case .unknown:
            return PhonePalette.smoke
        }
    }
}

extension MobileMonitorViewModel.ConnectionState {
    var title: String {
        switch self {
        case .idle, .loading:
            return "Connecting to your Mac"
        case .online:
            return "Ghost fire is live"
        case .notConfigured:
            return "Pair this iPhone"
        case .unauthorized:
            return "Pairing needs attention"
        case .unreachable:
            return "Mac host unreachable"
        case .failed:
            return "Connection issue"
        }
    }

    var message: String {
        switch self {
        case .idle, .loading:
            return "Onibi is warming up the mobile gateway and pulling your latest Ghostty activity."
        case .online:
            return "Session status, command completions, and assistant activity are flowing in from your Mac."
        case .notConfigured:
            return "Add the Tailscale host URL and pairing token from the macOS app to unlock live monitoring."
        case .unauthorized:
            return "The pairing token no longer matches the host. Update the saved connection to resume sync."
        case .unreachable:
            return "The saved Tailscale host could not be reached from this iPhone."
        case .failed(let message):
            return message
        }
    }

    var tintColor: Color {
        switch self {
        case .online:
            return PhonePalette.moss
        case .unreachable:
            return PhonePalette.rose
        case .unauthorized, .failed:
            return PhonePalette.sunrise
        default:
            return PhonePalette.cobalt
        }
    }

    var symbolName: String {
        switch self {
        case .online:
            return "dot.radiowaves.left.and.right"
        case .notConfigured:
            return "link.badge.plus"
        case .unauthorized:
            return "key.slash"
        case .unreachable:
            return "wifi.slash"
        case .failed:
            return "exclamationmark.triangle"
        case .idle, .loading:
            return "arrow.triangle.2.circlepath"
        }
    }
}
