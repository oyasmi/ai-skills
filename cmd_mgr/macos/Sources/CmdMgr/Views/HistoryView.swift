import SwiftUI
import AppKit

/// Displays the execution history of a command.
struct HistoryView: View {
    let history: [CommandHistory]

    private static let displayFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd HH:mm:ss"
        return f
    }()

    var body: some View {
        VStack(spacing: 0) {
            if history.isEmpty {
                Spacer()
                VStack(spacing: 12) {
                    Image(systemName: "clock")
                        .font(.system(size: 36))
                        .foregroundColor(.secondary)
                    Text("No execution history")
                        .font(.headline)
                        .foregroundColor(.secondary)
                }
                Spacer()
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 16) {
                        ForEach(history) { entry in
                            VStack(alignment: .leading, spacing: 6) {
                                // Header row
                                HStack(spacing: 6) {
                                    statusBadge(entry.status)
                                    Text(Self.displayFormatter.string(from: entry.startTime))
                                        .font(.system(size: 12, weight: .medium, design: .monospaced))
                                        .foregroundColor(.secondary)
                                    if let dur = durationString(entry) {
                                        Text("·")
                                            .foregroundColor(.secondary.opacity(0.5))
                                        Text(dur)
                                            .font(.system(size: 12, design: .monospaced))
                                            .foregroundColor(.secondary)
                                    }
                                    Spacer()
                                    if let out = entry.output, !out.isEmpty {
                                        Button {
                                            NSPasteboard.general.clearContents()
                                            NSPasteboard.general.setString(out, forType: .string)
                                        } label: {
                                            Image(systemName: "doc.on.doc")
                                                .font(.system(size: 11))
                                        }
                                        .buttonStyle(.plain)
                                        .foregroundColor(.secondary)
                                        .help("Copy output")
                                    }
                                }

                                // Output block
                                Text(entry.output ?? "[No output captured]")
                                    .font(.system(size: 11, design: .monospaced))
                                    .foregroundColor(entry.output != nil ? .primary : .secondary)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .padding(8)
                                    .background(
                                        RoundedRectangle(cornerRadius: 6)
                                            .fill(Color(nsColor: .textBackgroundColor))
                                    )
                                    .textSelection(.enabled)
                            }

                            if entry.id != history.last?.id {
                                Divider()
                            }
                        }
                    }
                    .padding(16)
                }
            }
        }
        .frame(minWidth: 500, minHeight: 300)
    }

    @ViewBuilder
    private func statusBadge(_ status: String) -> some View {
        let (color, icon): (Color, String) = {
            switch status {
            case "success":    return (.green,    "checkmark.circle.fill")
            case "failed":     return (.red,      "xmark.circle.fill")
            case "terminated": return (.orange,   "minus.circle.fill")
            case "running":    return (.blue,     "circle.fill")
            default:           return (.secondary,"questionmark.circle.fill")
            }
        }()

        Label(status.capitalized, systemImage: icon)
            .font(.system(size: 11, weight: .medium))
            .foregroundColor(color)
    }

    private func durationString(_ entry: CommandHistory) -> String? {
        guard let end = entry.endTime else { return nil }
        let secs = Int(end.timeIntervalSince(entry.startTime))
        if secs < 60 { return "\(secs)s" }
        let m = secs / 60, s = secs % 60
        if m < 60 { return "\(m)m \(s)s" }
        let h = m / 60, rm = m % 60
        return "\(h)h \(rm)m"
    }
}
