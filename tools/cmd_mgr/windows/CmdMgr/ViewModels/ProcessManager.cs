using System.Diagnostics;
using System.Collections.Concurrent;
using System.Text;

namespace CmdMgr.ViewModels;

public enum ProcessStatus
{
    Running,
    Success,
    Failed,
    Stopped
}

public static class ProcessStatusExtensions
{
    public static string DisplayName(this ProcessStatus status) => status switch
    {
        ProcessStatus.Running => "Running",
        ProcessStatus.Success => "Succeeded",
        ProcessStatus.Failed => "Failed",
        ProcessStatus.Stopped => "Stopped",
        _ => status.ToString()
    };

    public static string Icon(this ProcessStatus status) => status switch
    {
        ProcessStatus.Running => "●",
        ProcessStatus.Success => "✅",
        ProcessStatus.Failed => "❌",
        ProcessStatus.Stopped => "⏹",
        _ => "○"
    };

    public static string BrushKey(this ProcessStatus status) => status switch
    {
        ProcessStatus.Running => "PrimaryBrush",
        ProcessStatus.Success => "SuccessBrush",
        ProcessStatus.Failed => "DangerBrush",
        ProcessStatus.Stopped => "WarningBrush",
        _ => "MutedBrush"
    };
}

/// <summary>
/// Information about a running (or recently completed) process.
/// </summary>
public class ProcessInfo : ViewModelBase
{
    private const int MaxLines = 5000;

    public int CommandId { get; }
    public Models.Command Command { get; }
    public Process Process { get; }
    public int HistoryId { get; }

    private string _output = "";
    public string Output
    {
        get => _output;
        set => SetField(ref _output, value);
    }

    private ProcessStatus _status = ProcessStatus.Running;
    public ProcessStatus Status
    {
        get => _status;
        set
        {
            if (SetField(ref _status, value))
                OnPropertyChanged(nameof(IsRunning));
        }
    }

    public bool IsRunning => Status == ProcessStatus.Running;

    public ProcessInfo(Models.Command command, Process process, int historyId)
    {
        Command = command;
        CommandId = command.Id;
        Process = process;
        HistoryId = historyId;
    }

    /// <summary>Appends text and trims to MaxLines to prevent unbounded memory growth.</summary>
    public void AppendOutput(string text)
    {
        var combined = _output + text;
        var lines = combined.Split('\n');
        Output = lines.Length > MaxLines
            ? string.Join("\n", lines[^MaxLines..])
            : combined;
    }
}

/// <summary>
/// Manages subprocess lifecycle on Windows.
/// </summary>
public class ProcessManager : ViewModelBase
{
    public ConcurrentDictionary<int, ProcessInfo> RunningProcesses { get; } = new();
    // Keeps last completed run per command so output remains viewable after exit.
    public ConcurrentDictionary<int, ProcessInfo> CompletedProcesses { get; } = new();

    private readonly ConcurrentDictionary<int, bool> _requestedStops = new();

    private string? _lastError;
    public string? LastError
    {
        get => _lastError;
        set => SetField(ref _lastError, value);
    }

    /// <summary>Raised whenever a command's running/completed process info changes.</summary>
    public event Action<int>? ProcessesChanged;

    public bool IsRunning(int commandId) =>
        RunningProcesses.TryGetValue(commandId, out var info) && info.IsRunning;

    public ProcessInfo? GetProcessInfo(int commandId) =>
        RunningProcesses.TryGetValue(commandId, out var r) ? r :
        CompletedProcesses.TryGetValue(commandId, out var c) ? c : null;

    public bool HasOutput(int commandId) =>
        !string.IsNullOrEmpty(GetProcessInfo(commandId)?.Output);

    public ProcessInfo? Start(Models.Command command, Models.Database database)
    {
        if (IsRunning(command.Id)) return null;

        int? historyId = null;
        try
        {
            Encoding encoding;
            try
            {
                encoding = Encoding.GetEncoding(
                    System.Globalization.CultureInfo.CurrentCulture.TextInfo.OEMCodePage);
            }
            catch
            {
                encoding = Encoding.UTF8;
            }

            var psi = new ProcessStartInfo
            {
                FileName = "cmd.exe",
                Arguments = $"/c {command.CommandText}",
                UseShellExecute = false,
                RedirectStandardOutput = true,
                RedirectStandardError = true,
                CreateNoWindow = true,
                StandardOutputEncoding = encoding,
                StandardErrorEncoding = encoding
            };

            if (!string.IsNullOrWhiteSpace(command.WorkingDirectory))
                psi.WorkingDirectory = command.WorkingDirectory;

            historyId = database.AddHistoryEntry(command.Id, DateTime.Now, "running");
            var proc = new Process { StartInfo = psi, EnableRaisingEvents = true };
            var info = new ProcessInfo(command, proc, historyId.Value);

            proc.OutputDataReceived += (_, e) =>
            {
                if (e.Data != null)
                    System.Windows.Application.Current?.Dispatcher.Invoke(
                        () => info.AppendOutput(e.Data + "\n"));
            };
            proc.ErrorDataReceived += (_, e) =>
            {
                if (e.Data != null)
                    System.Windows.Application.Current?.Dispatcher.Invoke(
                        () => info.AppendOutput($"[STDERR] {e.Data}\n"));
            };

            // Exited is the single source of truth for all cleanup and DB writes.
            proc.Exited += (_, _) =>
            {
                var wasRequestedStop = _requestedStops.TryRemove(command.Id, out _);
                var status = wasRequestedStop ? "terminated" : (proc.ExitCode == 0 ? "success" : "failed");
                System.Windows.Application.Current?.Dispatcher.Invoke(() =>
                {
                    var finalStatus = wasRequestedStop ? ProcessStatus.Stopped
                        : (proc.ExitCode == 0 ? ProcessStatus.Success : ProcessStatus.Failed);
                    info.Status = finalStatus;
                    command.Status = finalStatus;
                    database.UpdateHistoryEntry(info.HistoryId, DateTime.Now, status, info.Output);
                    RunningProcesses.TryRemove(command.Id, out _);
                    CompletedProcesses[command.Id] = info;
                    ProcessesChanged?.Invoke(command.Id);
                });
            };

            command.Status = ProcessStatus.Running;
            CompletedProcesses.TryRemove(command.Id, out _);
            RunningProcesses[command.Id] = info;
            ProcessesChanged?.Invoke(command.Id);

            if (!proc.Start())
            {
                command.Status = ProcessStatus.Failed;
                RunningProcesses.TryRemove(command.Id, out _);
                var message = $"Could not start \"{command.Name}\": process did not start.";
                info.Status = ProcessStatus.Failed;
                info.AppendOutput(message);
                CompletedProcesses[command.Id] = info;
                LastError = message;
                database.UpdateHistoryEntry(historyId.Value, DateTime.Now, "failed", message);
                ProcessesChanged?.Invoke(command.Id);
                return null;
            }

            proc.BeginOutputReadLine();
            proc.BeginErrorReadLine();
            return info;
        }
        catch (Exception ex)
        {
            command.Status = ProcessStatus.Failed;
            RunningProcesses.TryRemove(command.Id, out _);
            var message = $"Could not start \"{command.Name}\": {ex.Message}";
            LastError = message;
            if (historyId.HasValue)
                database.UpdateHistoryEntry(historyId.Value, DateTime.Now, "failed", message);
            ProcessesChanged?.Invoke(command.Id);
            return null;
        }
    }

    /// <summary>
    /// Sends a kill signal. All cleanup (IsRunning, DB update, dictionary removal)
    /// is handled exclusively by the Exited event handler to avoid race conditions.
    /// </summary>
    public void Stop(int commandId, Models.Database database)
    {
        if (!RunningProcesses.TryGetValue(commandId, out var info)) return;

        _requestedStops[commandId] = true;
        try
        {
            if (!info.Process.HasExited)
                info.Process.Kill(entireProcessTree: true);
        }
        catch { /* Process already exited – Exited handler will clean up */ }
    }

    public void StopAll(Models.Database database)
    {
        foreach (var cmdId in RunningProcesses.Keys)
            Stop(cmdId, database);
    }
}
