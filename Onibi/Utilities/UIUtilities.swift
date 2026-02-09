import SwiftUI

// MARK: - Animations

/// Standard animation configurations
struct AppAnimations {
    static let quick = Animation.easeOut(duration: 0.15)
    static let standard = Animation.easeInOut(duration: 0.25)
    static let smooth = Animation.spring(response: 0.3, dampingFraction: 0.8)
    static let bounce = Animation.spring(response: 0.4, dampingFraction: 0.6)
    
    /// Check if user prefers reduced motion
    static var prefersReducedMotion: Bool {
        NSWorkspace.shared.accessibilityDisplayShouldReduceMotion
    }
    
    /// Get appropriate animation respecting accessibility settings
    static func adaptive(_ animation: Animation) -> Animation? {
        prefersReducedMotion ? nil : animation
    }
}

// MARK: - Micro-interactions

struct PressEffect: ViewModifier {
    @State private var isPressed = false
    
    func body(content: Content) -> some View {
        content
            .scaleEffect(isPressed ? 0.97 : 1.0)
            .animation(AppAnimations.quick, value: isPressed)
            .simultaneousGesture(
                DragGesture(minimumDistance: 0)
                    .onChanged { _ in isPressed = true }
                    .onEnded { _ in isPressed = false }
            )
    }
}

struct ShimmerEffect: ViewModifier {
    @State private var phase: CGFloat = 0
    var isAnimating: Bool = true
    
    func body(content: Content) -> some View {
        content
            .overlay(
                LinearGradient(
                    colors: [
                        .clear,
                        .white.opacity(0.2),
                        .clear
                    ],
                    startPoint: .leading,
                    endPoint: .trailing
                )
                .offset(x: phase * 400 - 200)
            )
            .clipped()
            .onAppear {
                startAnimation()
            }
            .onChange(of: isAnimating) { _, animating in
                if animating {
                    startAnimation()
                } else {
                    phase = 0
                }
            }
    }
    
    private func startAnimation() {
        guard isAnimating else { return }
        withAnimation(.linear(duration: 1.5).repeatForever(autoreverses: false)) {
            phase = 1
        }
    }
}

extension View {
    func pressEffect() -> some View {
        modifier(PressEffect())
    }
    
    func shimmer(isAnimating: Bool = true) -> some View {
        modifier(ShimmerEffect(isAnimating: isAnimating))
    }
}

// MARK: - Loading States

struct SkeletonView: View {
    var body: some View {
        RoundedRectangle(cornerRadius: 4)
            .fill(Color.secondary.opacity(0.15))
            .shimmer()
    }
}

struct NotificationCardSkeleton: View {
    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Circle()
                .fill(Color.secondary.opacity(0.15))
                .frame(width: 24, height: 24)
            
            VStack(alignment: .leading, spacing: 8) {
                SkeletonView()
                    .frame(width: 180, height: 14)
                SkeletonView()
                    .frame(width: 240, height: 12)
            }
            
            Spacer()
            
            SkeletonView()
                .frame(width: 40, height: 12)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
    }
}

// MARK: - Error States

struct ErrorView: View {
    let title: String
    let message: String
    let retryAction: (() -> Void)?
    
    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "exclamationmark.triangle")
                .font(.system(size: 48))
                .foregroundColor(.orange)
            
            VStack(spacing: 8) {
                Text(title)
                    .font(.headline)
                
                Text(message)
                    .font(.caption)
                    .foregroundColor(.secondary)
                    .multilineTextAlignment(.center)
            }
            
            if let retry = retryAction {
                Button("Try Again", action: retry)
                    .buttonStyle(.borderedProminent)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }
}

// MARK: - Success Confirmation

struct SuccessToast: View {
    let message: String
    @Binding var isShowing: Bool
    var duration: TimeInterval = 2.0
    
    var body: some View {
        if isShowing {
            HStack(spacing: 8) {
                Image(systemName: "checkmark.circle.fill")
                    .foregroundColor(.green)
                Text(message)
                    .font(.subheadline)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
            .background(Color(NSColor.controlBackgroundColor))
            .cornerRadius(20)
            .shadow(radius: 4)
            .transition(.move(edge: .top).combined(with: .opacity))
            .onAppear {
                DispatchQueue.main.asyncAfter(deadline: .now() + duration) {
                    withAnimation { isShowing = false }
                }
            }
        }
    }
}

// MARK: - Accessibility

struct AccessibilityModifier: ViewModifier {
    let label: String
    let hint: String?
    let traits: AccessibilityTraits
    
    func body(content: Content) -> some View {
        content
            .accessibilityLabel(label)
            .accessibilityHint(hint ?? "")
            .accessibilityAddTraits(traits)
    }
}

extension View {
    func accessible(_ label: String, hint: String? = nil, traits: AccessibilityTraits = []) -> some View {
        modifier(AccessibilityModifier(label: label, hint: hint, traits: traits))
    }
}

// MARK: - Dynamic Type

struct ScaledFont: ViewModifier {
    @Environment(\.sizeCategory) var sizeCategory
    let size: CGFloat
    let weight: Font.Weight
    
    func body(content: Content) -> some View {
        content
            .font(.system(size: scaledSize, weight: weight))
    }
    
    private var scaledSize: CGFloat {
        // Scale factor based on accessibility size category
        let factor: CGFloat
        switch sizeCategory {
        case .extraSmall: factor = 0.8
        case .small: factor = 0.9
        case .medium: factor = 1.0
        case .large: factor = 1.1
        case .extraLarge: factor = 1.2
        case .extraExtraLarge: factor = 1.3
        case .extraExtraExtraLarge: factor = 1.4
        case .accessibilityMedium: factor = 1.5
        case .accessibilityLarge: factor = 1.6
        case .accessibilityExtraLarge: factor = 1.8
        case .accessibilityExtraExtraLarge: factor = 2.0
        case .accessibilityExtraExtraExtraLarge: factor = 2.2
        @unknown default: factor = 1.0
        }
        return size * factor
    }
}

extension View {
    func scaledFont(size: CGFloat, weight: Font.Weight = .regular) -> some View {
        modifier(ScaledFont(size: size, weight: weight))
    }
}

// MARK: - High Contrast

struct HighContrastModifier: ViewModifier {
    @Environment(\.accessibilityHighContrast) var highContrast
    let normalColor: Color
    let highContrastColor: Color
    
    func body(content: Content) -> some View {
        content
            .foregroundColor(highContrast ? highContrastColor : normalColor)
    }
}

// MARK: - Dark Mode Optimization

extension Color {
    /// OLED-optimized pure black
    static var oledBlack: Color {
        Color(red: 0, green: 0, blue: 0)
    }
    
    /// Slightly lighter black for depth
    static var oledDark: Color {
        Color(red: 0.05, green: 0.05, blue: 0.05)
    }
    
    /// Initialize from hex string
    init(hex: String) {
        let hex = hex.trimmingCharacters(in: CharacterSet.alphanumerics.inverted)
        var int: UInt64 = 0
        Scanner(string: hex).scanHexInt64(&int)
        let a, r, g, b: UInt64
        switch hex.count {
        case 3: // RGB (12-bit)
            (a, r, g, b) = (255, (int >> 8) * 17, (int >> 4 & 0xF) * 17, (int & 0xF) * 17)
        case 6: // RGB (24-bit)
            (a, r, g, b) = (255, int >> 16, int >> 8 & 0xFF, int & 0xFF)
        case 8: // ARGB (32-bit)
            (a, r, g, b) = (int >> 24, int >> 16 & 0xFF, int >> 8 & 0xFF, int & 0xFF)
        default:
            (a, r, g, b) = (255, 0, 0, 0)
        }
        self.init(
            .sRGB,
            red: Double(r) / 255,
            green: Double(g) / 255,
            blue:  Double(b) / 255,
            opacity: Double(a) / 255
        )
    }
}

struct ColorPreview: View {
    let hex: String
    
    var body: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(Color(hex: hex))
                .frame(width: 12, height: 12)
                .overlay(Circle().stroke(Color.secondary.opacity(0.3), lineWidth: 1))
            Text(hex)
                .font(.caption)
                .foregroundColor(.secondary)
                .monospaced()
        }
    }
}

// MARK: - Visual Feedback

struct RippleEffect: ViewModifier {
    @State private var isAnimating = false
    let trigger: Bool
    
    func body(content: Content) -> some View {
        content
            .overlay(
                Circle()
                    .stroke(Color.accentColor.opacity(isAnimating ? 0 : 0.5), lineWidth: 2)
                    .scaleEffect(isAnimating ? 2 : 1)
                    .opacity(isAnimating ? 0 : 1)
            )
            .onChange(of: trigger) { _, newValue in
                if newValue {
                    withAnimation(.easeOut(duration: 0.4)) {
                        isAnimating = true
                    }
                    DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) {
                        isAnimating = false
                    }
                }
            }
    }
}

extension View {
    func ripple(trigger: Bool) -> some View {
        modifier(RippleEffect(trigger: trigger))
    }
}

// MARK: - Sound Effects

final class SoundEffects {
    static let shared = SoundEffects()
    
    private var enabled = true
    private let soundQueue = DispatchQueue(label: "com.onibi.soundeffects", qos: .utility)
    
    func play(_ sound: SystemSound) {
        guard enabled else { return }
        
        soundQueue.async {
            switch sound {
            case .click:
                NSSound(named: "Pop")?.play()
            case .success:
                NSSound(named: "Glass")?.play()
            case .notification:
                NSSound(named: "Ping")?.play()
            case .error:
                NSSound(named: "Basso")?.play()
            }
        }
    }
    
    func setEnabled(_ value: Bool) {
        enabled = value
    }
    
    enum SystemSound {
        case click
        case success
        case notification
        case error
    }
}
