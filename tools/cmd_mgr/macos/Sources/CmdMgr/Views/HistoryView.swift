import SwiftUI
import AppKit

struct HistoryView: View {
    let history: [CommandHistory]
    let runAgain: () -> Void
    @State private var expandedEntries: Set<Int> = []
    @State private var copiedEntryID: Int?

    private static let displayFormatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .medium
        return formatter
    }()

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text(history.isEmpty ? "No runs" : "\(history.count) run\(history.count == 1 ? "" : "s")")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Spacer()
                Button(action: runAgain) {
                    Label("Run Again", systemImage: "play.fill")
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 9)

            Divider()

            if history.isEmpty {
                VStack(spacing: 12) {
                    Image(systemName: "clock.arrow.circlepath")
                        .font(.system(size: 36))
                        .foregroundStyle(.secondary)
                    Text("No Execution History")
                        .font(.headline)
                    Text("Completed runs will appear here.")
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                List(history) { entry in
                    DisclosureGroup(isExpanded: expansionBinding(for: entry.id)) {
                        outputBlock(for: entry)
                    } label: {
                        historyLabel(for: entry)
                    }
                    .padding(.vertical, 5)
                }
                .listStyle(.inset)
            }
        }
    }

    private func historyLabel(for entry: CommandHistory) -> some View {
        HStack(spacing: 10) {
            Label(statusLabel(entry.status), systemImage: statusIcon(entry.status))
                .foregroundStyle(statusColor(entry.status))
                .font(.subheadline.weight(.medium))
                .frame(width: 105, alignment: .leading)

            Text(Self.displayFormatter.string(from: entry.startTime))
                .font(.subheadline)
                .foregroundStyle(.secondary)

            Spacer()

            if let duration = durationString(entry) {
                Text(duration)
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(.secondary)
            }
        }
    }

    private func outputBlock(for entry: CommandHistory) -> some View {
        VStack(alignment: .trailing, spacing: 8) {
            ScrollView(.horizontal) {
                Text(entry.output?.isEmpty == false ? entry.output! : "No output captured")
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(entry.output?.isEmpty == false ? .primary : .secondary)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(10)
            }
            .frame(maxHeight: 220)
            .background(Color(nsColor: .textBackgroundColor),
                        in: RoundedRectangle(cornerRadius: 7))

            if let output = entry.output, !output.isEmpty {
                Button {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(output, forType: .string)
                    copiedEntryID = entry.id
                    DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) {
                        if copiedEntryID == entry.id { copiedEntryID = nil }
                    }
                } label: {
                    Label(copiedEntryID == entry.id ? "Copied" : "Copy Output",
                          systemImage: copiedEntryID == entry.id ? "checkmark" : "doc.on.doc")
                }
                .buttonStyle(.borderless)
            }
        }
        .padding(.leading, 28)
        .padding(.top, 8)
    }

    private func expansionBinding(for id: Int) -> Binding<Bool> {
        Binding(
            get: { expandedEntries.contains(id) },
            set: { expanded in
                if expanded { expandedEntries.insert(id) }
                else { expandedEntries.remove(id) }
            })
    }

    private func statusColor(_ status: String) -> Color {
        switch status {
        case "success": return .green
        case "failed": return .red
        case "terminated": return .orange
        case "running": return .blue
        default: return .secondary
        }
    }

    private func statusLabel(_ status: String) -> String {
        status == "terminated" ? "Stopped" : status.capitalized
    }

    private func statusIcon(_ status: String) -> String {
        switch status {
        case "success": return "checkmark.circle.fill"
        case "failed": return "xmark.circle.fill"
        case "terminated": return "stop.circle.fill"
        case "running": return "waveform.path"
        default: return "questionmark.circle"
        }
    }

    private func durationString(_ entry: CommandHistory) -> String? {
        guard let end = entry.endTime else { return nil }
        let seconds = max(0, Int(end.timeIntervalSince(entry.startTime)))
        if seconds < 60 { return "\(seconds)s" }
        let minutes = seconds / 60
        if minutes < 60 { return "\(minutes)m \(seconds % 60)s" }
        return "\(minutes / 60)h \(minutes % 60)m"
    }
}
