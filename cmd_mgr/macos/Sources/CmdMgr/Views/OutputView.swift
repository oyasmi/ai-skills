import SwiftUI
import AppKit

/// NSTextView wrapper – handles large/frequently-updating output efficiently.
/// Only redraws when text actually changes, and only auto-scrolls when the user
/// is already at the bottom (doesn't yank the scroll position away mid-read).
struct TerminalTextView: NSViewRepresentable {
    let text: String

    func makeNSView(context: Context) -> NSScrollView {
        let scrollView = NSTextView.scrollableTextView()
        guard let textView = scrollView.documentView as? NSTextView else { return scrollView }
        textView.isEditable = false
        textView.isSelectable = true
        textView.font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
        textView.backgroundColor = NSColor.textBackgroundColor
        textView.textContainerInset = NSSize(width: 8, height: 8)
        textView.autoresizingMask = [.width]
        scrollView.hasVerticalScroller = true
        scrollView.autohidesScrollers = true
        return scrollView
    }

    func updateNSView(_ scrollView: NSScrollView, context: Context) {
        guard let textView = scrollView.documentView as? NSTextView else { return }
        guard textView.string != text else { return }

        let atBottom = isNearBottom(scrollView)
        textView.string = text
        if atBottom {
            textView.scrollToEndOfDocument(nil)
        }
    }

    private func isNearBottom(_ scrollView: NSScrollView) -> Bool {
        guard let doc = scrollView.documentView else { return true }
        let visibleMaxY = scrollView.documentVisibleRect.maxY
        return (doc.frame.height - visibleMaxY) < 30
    }
}

/// Real-time output view for a running or completed process.
struct OutputView: View {
    @ObservedObject var processInfo: CommandProcess

    var body: some View {
        VStack(spacing: 0) {
            // Status bar
            HStack {
                Circle()
                    .fill(processInfo.isRunning ? Color.green : Color.secondary.opacity(0.3))
                    .frame(width: 8, height: 8)
                Text(processInfo.isRunning ? "Running" : "Terminated")
                    .font(.subheadline)
                    .foregroundColor(.secondary)
                Spacer()
                if !processInfo.output.isEmpty {
                    Button {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(processInfo.output, forType: .string)
                    } label: {
                        Label("Copy All", systemImage: "doc.on.doc")
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 8)

            Divider()

            if processInfo.output.isEmpty {
                Spacer()
                Text("Waiting for output…")
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundColor(.secondary)
                Spacer()
            } else {
                TerminalTextView(text: processInfo.output)
            }
        }
        .frame(minWidth: 500, minHeight: 300)
    }
}
