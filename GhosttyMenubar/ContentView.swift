import SwiftUI

struct ContentView: View {
    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text("Ghostty Menubar")
                    .font(.headline)
                    .fontWeight(.semibold)
                Spacer()
                Button(action: {}) {
                    Image(systemName: "gearshape")
                        .foregroundColor(.secondary)
                }
                .buttonStyle(.plain)
            }
            .padding()
            .background(Color(NSColor.controlBackgroundColor))
            
            Divider()
            
            // Content
            VStack {
                Spacer()
                Image(systemName: "terminal")
                    .font(.system(size: 48))
                    .foregroundColor(.secondary)
                Text("No notifications")
                    .font(.subheadline)
                    .foregroundColor(.secondary)
                    .padding(.top, 8)
                Spacer()
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            
            Divider()
            
            // Footer
            HStack {
                Button("View Logs") {}
                    .buttonStyle(.plain)
                    .foregroundColor(.accentColor)
                Spacer()
                Button("Quit") {
                    NSApplication.shared.terminate(nil)
                }
                .buttonStyle(.plain)
                .foregroundColor(.secondary)
            }
            .padding()
            .background(Color(NSColor.controlBackgroundColor))
        }
        .frame(width: 360, height: 480)
    }
}

#Preview {
    ContentView()
}
