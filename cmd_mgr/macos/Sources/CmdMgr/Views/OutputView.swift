import SwiftUI
import AppKit

/// NSTextView wrapper optimized for frequently updating command output.
struct TerminalTextView: NSViewRepresentable {
    let text: String
    let wrapsLines: Bool
    let followsOutput: Bool

    func makeNSView(context: Context) -> NSScrollView {
        let scrollView = NSTextView.scrollableTextView()
        guard let textView = scrollView.documentView as? NSTextView else { return scrollView }
        textView.isEditable = false
        textView.isSelectable = true
        textView.font = NSFont.monospacedSystemFont(ofSize: 12, weight: .regular)
        textView.backgroundColor = NSColor.textBackgroundColor
        textView.textContainerInset = NSSize(width: 12, height: 10)
        textView.usesFindBar = true
        textView.isIncrementalSearchingEnabled = true
        scrollView.hasVerticalScroller = true
        scrollView.autohidesScrollers = true
        configureWrapping(textView: textView, scrollView: scrollView)
        return scrollView
    }

    func updateNSView(_ scrollView: NSScrollView, context: Context) {
        guard let textView = scrollView.documentView as? NSTextView else { return }
        configureWrapping(textView: textView, scrollView: scrollView)
        guard textView.string != text else { return }

        let wasNearBottom = isNearBottom(scrollView)
        textView.string = text
        if followsOutput || wasNearBottom {
            textView.scrollToEndOfDocument(nil)
        }
    }

    private func configureWrapping(textView: NSTextView, scrollView: NSScrollView) {
        textView.isHorizontallyResizable = !wrapsLines
        textView.textContainer?.widthTracksTextView = wrapsLines
        textView.textContainer?.containerSize = NSSize(
            width: wrapsLines ? scrollView.contentSize.width : .greatestFiniteMagnitude,
            height: .greatestFiniteMagnitude)
        scrollView.hasHorizontalScroller = !wrapsLines
    }

    private func isNearBottom(_ scrollView: NSScrollView) -> Bool {
        guard let documentView = scrollView.documentView else { return true }
        return documentView.frame.height - scrollView.documentVisibleRect.maxY < 30
    }
}

struct OutputView: View {
    let processInfo: CommandProcess?
    let runAction: () -> Void

    var body: some View {
        if let processInfo {
            ProcessOutputView(processInfo: processInfo)
        } else {
            VStack(spacing: 12) {
                Image(systemName: "text.alignleft")
                    .font(.system(size: 36))
                    .foregroundStyle(.secondary)
                Text("No Output Yet")
                    .font(.headline)
                Text("Run this command to see its output here.")
                    .foregroundStyle(.secondary)
                Button("Run Command", action: runAction)
                    .buttonStyle(.borderedProminent)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
}

private struct ProcessOutputView: View {
    @ObservedObject var processInfo: CommandProcess
    @State private var wrapsLines = true
    @State private var followsOutput = true
    @State private var didCopy = false

    var body: some View {
        VStack(spacing: 0) {
            consoleToolbar
            Divider()

            if processInfo.output.isEmpty {
                emptyOutput(status: processInfo.status)
            } else {
                TerminalTextView(text: processInfo.output, wrapsLines: wrapsLines,
                                 followsOutput: followsOutput)
            }
        }
    }

    private var consoleToolbar: some View {
        HStack(spacing: 12) {
            Label(processInfo.status.displayName, systemImage: processInfo.status.systemImage)
                .font(.subheadline.weight(.medium))
                .foregroundStyle(processInfo.status.color)

            Spacer()

            Toggle("Follow", isOn: $followsOutput)
                .toggleStyle(.checkbox)
                .help("Keep the latest output visible")
            Toggle("Wrap", isOn: $wrapsLines)
                .toggleStyle(.checkbox)
                .help("Wrap long lines")

            Button {
                copyAll()
            } label: {
                Label(didCopy ? "Copied" : "Copy All",
                      systemImage: didCopy ? "checkmark" : "doc.on.doc")
            }
            .disabled(processInfo.output.isEmpty)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 9)
    }

    private func emptyOutput(status: ProcessStatus) -> some View {
        VStack(spacing: 10) {
            ProgressView()
                .controlSize(.small)
                .opacity(status == .running ? 1 : 0)
            Text(status == .running ? "Waiting for output…" : "No output was captured.")
                .font(.system(.body, design: .monospaced))
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func copyAll() {
        let output = processInfo.output
        guard !output.isEmpty else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(output, forType: .string)
        didCopy = true
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) { didCopy = false }
    }
}
