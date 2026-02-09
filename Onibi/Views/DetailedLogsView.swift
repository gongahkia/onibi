import SwiftUI

/// Tab options for log viewing modes
enum LogViewTab: String, CaseIterable {
    case all = "All"
    case commands = "Commands"
    case ai = "AI"
    case workflows = "Workflows"
    
    var icon: String {
        switch self {
        case .all: return "list.bullet"
        case .commands: return "terminal"
        case .ai: return "sparkles"
        case .workflows: return "hammer"
        }
    }
}

/// Detailed logs viewer view
struct DetailedLogsView: View {
    @StateObject private var viewModel = LogsViewModel()
    @State private var selectedTab: LogViewTab = .all
    @State private var searchText: String = ""
    @State private var showExportOptions = false
    @State private var showFilterSheet = false
    @State private var selectedLogEntry: LogEntry?
    @State private var filteredLogs: [LogEntry] = []
    
    var body: some View {
        VStack(spacing: 0) {
            // Header with tabs
            headerSection
            
            Divider()
            
            // Search and filters
            searchAndFilterSection
            
            Divider()
            
            // Logs content
            if viewModel.isLoading {
                loadingView
            } else if filteredLogs.isEmpty {
                emptyStateView
            } else {
                logsList
            }
            
            Divider()
            
            // Footer with stats
            footerSection
        }
        .frame(minWidth: 500, minHeight: 400)
        .sheet(item: $selectedLogEntry) { entry in
            LogEntryDetailView(entry: entry)
        }
        .sheet(isPresented: $showExportOptions) {
            ExportOptionsView(logs: filteredLogs)
        }
        .sheet(isPresented: $showFilterSheet) {
            FilterSheetView(viewModel: viewModel)
        }
        .onAppear {
            updateFilteredLogs()
        }
        .onChange(of: viewModel.logs) { _ in
            updateFilteredLogs()
        }
        .onChange(of: selectedTab) { _ in
            updateFilteredLogs()
        }
        .onChange(of: searchText) { _ in
            updateFilteredLogs()
        }
    }
    
    // MARK: - Filtered Logs
    
    private func updateFilteredLogs() {
        var logs = viewModel.logs
        
        // Filter by tab
        switch selectedTab {
        case .all:
            break
        case .commands:
            logs = logs.filter { !$0.command.isEmpty }
        case .ai:
            logs = logs.filter { $0.metadata["source"] == "ai" }
        case .workflows:
            logs = logs.filter { $0.metadata["source"] == "workflow" }
        }
        
        // Filter by search
        if !searchText.isEmpty {
            logs = logs.filter {
                $0.command.localizedCaseInsensitiveContains(searchText) ||
                $0.output.localizedCaseInsensitiveContains(searchText)
            }
        }
        
        filteredLogs = logs
    }
    
    // MARK: - Header
    
    private var headerSection: some View {
        HStack(spacing: 16) {
            Text("Logs")
                .font(.title2)
                .fontWeight(.semibold)
            
            Spacer()
            
            // Tab picker
            Picker("View", selection: $selectedTab) {
                ForEach(LogViewTab.allCases, id: \.self) { tab in
                    Label(tab.rawValue, systemImage: tab.icon)
                        .tag(tab)
                }
            }
            .pickerStyle(.segmented)
            .frame(width: 300)
        }
        .padding()
    }
    
    // MARK: - Search & Filters
    
    private var searchAndFilterSection: some View {
        HStack(spacing: 12) {
            // Search bar
            HStack {
                Image(systemName: "magnifyingglass")
                    .foregroundColor(.secondary)
                TextField("Search logs...", text: $searchText)
                    .textFieldStyle(.plain)
                if !searchText.isEmpty {
                    Button(action: { searchText = "" }) {
                        Image(systemName: "xmark.circle.fill")
                            .foregroundColor(.secondary)
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(8)
            .background(Color(NSColor.controlBackgroundColor))
            .cornerRadius(8)
            
            // Filter button
            Button(action: { showFilterSheet = true }) {
                HStack(spacing: 4) {
                    Image(systemName: "line.3.horizontal.decrease.circle")
                    Text("Filters")
                }
            }
            .buttonStyle(.bordered)
            
            // Active filter chips
            if !viewModel.activeFilters.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 6) {
                        ForEach(viewModel.activeFilters, id: \.self) { filter in
                            FilterChip(text: filter) {
                                viewModel.removeFilter(filter)
                            }
                        }
                    }
                }
                .frame(maxWidth: 200)
            }
            
            Spacer()
            
            // Export button
            Button(action: { showExportOptions = true }) {
                HStack(spacing: 4) {
                    Image(systemName: "square.and.arrow.up")
                    Text("Export")
                }
            }
            .buttonStyle(.bordered)
        }
        .padding(.horizontal)
        .padding(.vertical, 8)
    }
    
    // MARK: - Logs List
    
    private var logsList: some View {
        ScrollView {
            LazyVStack(spacing: 1) {
                ForEach(filteredLogs) { log in
                    LogEntryRow(entry: log, searchText: searchText)
                        .onTapGesture {
                            selectedLogEntry = log
                        }
                }
            }
            .padding(.vertical, 4)
        }
    }
    
    // MARK: - Loading
    
    private var loadingView: some View {
        VStack(spacing: 16) {
            ProgressView()
                .scaleEffect(1.2)
            Text("Loading logs...")
                .font(.subheadline)
                .foregroundColor(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
    
    // MARK: - Empty State
    
    private var emptyStateView: some View {
        VStack(spacing: 16) {
            Image(systemName: "doc.text.magnifyingglass")
                .font(.system(size: 48))
                .foregroundColor(.secondary.opacity(0.5))
            Text("No logs found")
                .font(.headline)
                .foregroundColor(.secondary)
            Text("Try adjusting your search or filters")
                .font(.caption)
                .foregroundColor(.secondary.opacity(0.7))
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
    
    // MARK: - Footer
    
    private var footerSection: some View {
        HStack {
            Text("\(filteredLogs.count) entries")
                .font(.caption)
                .foregroundColor(.secondary)
            
            Spacer()
            
            if let first = filteredLogs.last, let last = filteredLogs.first {
                Text("\(formatDate(first.timestamp)) - \(formatDate(last.timestamp))")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }
        }
        .padding(.horizontal)
        .padding(.vertical, 10)
        .background(Color(NSColor.controlBackgroundColor))
    }
    
    private func formatDate(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.dateStyle = .short
        formatter.timeStyle = .short
        return formatter.string(from: date)
    }
}

// MARK: - Log Entry Row

struct LogEntryRow: View {
    let entry: LogEntry
    let searchText: String
    
    @State private var isExpanded = false
    @State private var isHovered = false
    
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .top) {
                // Timestamp
                Text(timeString)
                    .font(.system(.caption, design: .monospaced))
                    .foregroundColor(.secondary)
                    .frame(width: 70, alignment: .leading)
                
                // Command with highlighting
                highlightedText(entry.command, highlight: searchText)
                    .font(.system(.body, design: .monospaced))
                    .lineLimit(isExpanded ? nil : 1)
                
                Spacer()
                
                // Exit code badge
                if let exitCode = entry.exitCode {
                    Text(exitCode == 0 ? "✓" : "✗ \(exitCode)")
                        .font(.caption)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(exitCode == 0 ? Color.green.opacity(0.2) : Color.red.opacity(0.2))
                        .foregroundColor(exitCode == 0 ? .green : .red)
                        .cornerRadius(4)
                }
                
                // Duration
                if let duration = entry.duration {
                    Text(formatDuration(duration))
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
                
                // Expand/Copy buttons on hover
                if isHovered {
                    HStack(spacing: 4) {
                        Button(action: { isExpanded.toggle() }) {
                            Image(systemName: isExpanded ? "chevron.up" : "chevron.down")
                                .font(.caption)
                        }
                        .buttonStyle(.plain)
                        
                        Button(action: copyToClipboard) {
                            Image(systemName: "doc.on.doc")
                                .font(.caption)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
            
            // Output (expanded)
            if isExpanded && !entry.output.isEmpty {
                Text(entry.output)
                    .font(.system(.caption, design: .monospaced))
                    .foregroundColor(.secondary)
                    .padding(8)
                    .background(Color(NSColor.textBackgroundColor).opacity(0.5))
                    .cornerRadius(6)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(
            isHovered ? Color(NSColor.selectedContentBackgroundColor).opacity(0.05) : Color.clear
        )
        .onHover { hovering in
            isHovered = hovering
        }
    }
    
    private var timeString: String {
        let formatter = DateFormatter()
        formatter.timeStyle = .medium
        return formatter.string(from: entry.timestamp)
    }
    
    private func formatDuration(_ duration: TimeInterval) -> String {
        if duration < 1 {
            return String(format: "%.0fms", duration * 1000)
        } else if duration < 60 {
            return String(format: "%.1fs", duration)
        } else {
            let minutes = Int(duration / 60)
            let seconds = Int(duration.truncatingRemainder(dividingBy: 60))
            return "\(minutes)m \(seconds)s"
        }
    }
    
    private func highlightedText(_ text: String, highlight: String) -> Text {
        guard !highlight.isEmpty else { return Text(text) }
        
        var result = Text("")
        var remaining = text[...]
        
        while let range = remaining.range(of: highlight, options: .caseInsensitive) {
            result = result + Text(remaining[..<range.lowerBound])
            result = result + Text(remaining[range]).bold().foregroundColor(.accentColor)
            remaining = remaining[range.upperBound...]
        }
        result = result + Text(remaining)
        
        return result
    }
    
    private func copyToClipboard() {
        let content = entry.output.isEmpty ? entry.command : "\(entry.command)\n\(entry.output)"
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(content, forType: .string)
    }
}

// MARK: - Filter Chip

struct FilterChip: View {
    let text: String
    let onRemove: () -> Void
    
    var body: some View {
        HStack(spacing: 4) {
            Text(text)
                .font(.caption)
            Button(action: onRemove) {
                Image(systemName: "xmark")
                    .font(.system(size: 8, weight: .bold))
            }
            .buttonStyle(.plain)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(Color.accentColor.opacity(0.15))
        .foregroundColor(.accentColor)
        .cornerRadius(12)
    }
}

// MARK: - Log Entry Detail View

struct LogEntryDetailView: View {
    let entry: LogEntry
    @Environment(\.dismiss) private var dismiss
    
    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Text("Log Entry Details")
                    .font(.title2)
                    .fontWeight(.semibold)
                Spacer()
                Button("Done") { dismiss() }
            }
            
            Divider()
            
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    detailRow("Command", value: entry.command)
                    detailRow("Timestamp", value: formatDate(entry.timestamp))
                    if let exitCode = entry.exitCode {
                        detailRow("Exit Code", value: String(exitCode))
                    }
                    if let duration = entry.duration {
                        detailRow("Duration", value: formatDuration(duration))
                    }
                    if let dir = entry.workingDirectory {
                        detailRow("Working Directory", value: dir)
                    }
                    
                    if !entry.output.isEmpty {
                        Text("Output")
                            .font(.headline)
                            .padding(.top)
                        
                        ScrollView(.horizontal, showsIndicators: true) {
                            Text(entry.output)
                                .font(.system(.body, design: .monospaced))
                                .textSelection(.enabled)
                        }
                        .padding()
                        .background(Color(NSColor.textBackgroundColor))
                        .cornerRadius(8)
                    }
                }
            }
            
            HStack {
                Button("Copy Command") {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(entry.command, forType: .string)
                }
                if !entry.output.isEmpty {
                    Button("Copy Output") {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(entry.output, forType: .string)
                    }
                }
                Spacer()
            }
        }
        .padding()
        .frame(minWidth: 500, minHeight: 400)
    }
    
    private func detailRow(_ label: String, value: String) -> some View {
        HStack(alignment: .top) {
            Text(label)
                .font(.subheadline)
                .foregroundColor(.secondary)
                .frame(width: 120, alignment: .trailing)
            Text(value)
                .font(.subheadline)
                .textSelection(.enabled)
        }
    }
    
    private func formatDate(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.dateStyle = .long
        formatter.timeStyle = .long
        return formatter.string(from: date)
    }
    
    private func formatDuration(_ duration: TimeInterval) -> String {
        if duration < 1 {
            return String(format: "%.0f ms", duration * 1000)
        } else {
            return String(format: "%.2f seconds", duration)
        }
    }
}

// MARK: - Export Options View

struct ExportOptionsView: View {
    let logs: [LogEntry]
    @Environment(\.dismiss) private var dismiss
    @State private var format: ExportFormat = .json
    @State private var isExporting = false
    
    enum ExportFormat: String, CaseIterable {
        case json = "JSON"
        case csv = "CSV"
        case txt = "Plain Text"
    }
    
    var body: some View {
        VStack(spacing: 20) {
            Text("Export Logs")
                .font(.title2)
                .fontWeight(.semibold)
            
            Picker("Format", selection: $format) {
                ForEach(ExportFormat.allCases, id: \.self) { format in
                    Text(format.rawValue).tag(format)
                }
            }
            .pickerStyle(.segmented)
            .disabled(isExporting)
            
            if isExporting {
                HStack {
                    ProgressView()
                        .scaleEffect(0.8)
                    Text("Exporting...")
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
            } else {
                Text("\(logs.count) entries will be exported")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }
            
            HStack {
                Button("Cancel") { dismiss() }
                    .disabled(isExporting)
                Button("Export") {
                    exportLogs()
                }
                .buttonStyle(.borderedProminent)
                .disabled(isExporting)
            }
        }
        .padding()
        .frame(width: 300)
    }
    
    private func exportLogs() {
        let panel = NSSavePanel()
        panel.allowedContentTypes = [.json, .commaSeparatedText, .plainText]
        panel.nameFieldStringValue = "ghostty-logs.\(format.rawValue.lowercased())"
        
        guard panel.runModal() == .OK, let url = panel.url else { return }
        
        isExporting = true
        
        Task {
            let content: String
            switch format {
            case .json:
                let encoder = JSONEncoder()
                encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
                encoder.dateEncodingStrategy = .iso8601
                if let data = try? encoder.encode(logs), let str = String(data: data, encoding: .utf8) {
                    content = str
                } else {
                    content = "[]"
                }
            case .csv:
                var csv = "timestamp,command,output,exit_code,duration\n"
                for log in logs {
                    let escaped = { (s: String) in s.replacingOccurrences(of: "\"", with: "\"\"") }
                    csv += "\"\(log.timestamp.ISO8601Format())\",\"\(escaped(log.command))\",\"\(escaped(log.output))\",\(log.exitCode ?? 0),\(log.duration ?? 0)\n"
                }
                content = csv
            case .txt:
                content = logs.map { "[\($0.timestamp)] $ \($0.command)\n\($0.output)" }.joined(separator: "\n\n---\n\n")
            }
            
            try? content.write(to: url, atomically: true, encoding: .utf8)
            
            await MainActor.run {
                isExporting = false
                dismiss()
            }
        }
    }
}

// MARK: - Filter Sheet View

struct FilterSheetView: View {
    @ObservedObject var viewModel: LogsViewModel
    @Environment(\.dismiss) private var dismiss
    @State private var dateFrom: Date = Calendar.current.date(byAdding: .day, value: -7, to: Date()) ?? Date()
    @State private var dateTo: Date = Date()
    @State private var showErrors: Bool = true
    @State private var showSuccess: Bool = true
    
    var body: some View {
        VStack(spacing: 20) {
            Text("Filter Logs")
                .font(.title2)
                .fontWeight(.semibold)
            
            Form {
                Section("Date Range") {
                    DatePicker("From", selection: $dateFrom, displayedComponents: [.date, .hourAndMinute])
                    DatePicker("To", selection: $dateTo, displayedComponents: [.date, .hourAndMinute])
                }
                
                Section("Status") {
                    Toggle("Show Errors", isOn: $showErrors)
                    Toggle("Show Success", isOn: $showSuccess)
                }
            }
            .formStyle(.grouped)
            
            HStack {
                Button("Reset") {
                    dateFrom = Calendar.current.date(byAdding: .day, value: -7, to: Date()) ?? Date()
                    dateTo = Date()
                    showErrors = true
                    showSuccess = true
                    viewModel.clearFilters()
                }
                Spacer()
                Button("Cancel") { dismiss() }
                Button("Apply") {
                    applyFilters()
                    dismiss()
                }
                .buttonStyle(.borderedProminent)
            }
        }
        .padding()
        .frame(width: 400, height: 350)
    }
    
    private func applyFilters() {
        viewModel.clearFilters()
        viewModel.addFilter("Date: \(formatDate(dateFrom)) - \(formatDate(dateTo))")
        if !showErrors {
            viewModel.addFilter("Hide Errors")
        }
        if !showSuccess {
            viewModel.addFilter("Hide Success")
        }
    }
    
    private func formatDate(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.dateStyle = .short
        return formatter.string(from: date)
    }
}

// MARK: - Preview

#Preview {
    DetailedLogsView()
}
