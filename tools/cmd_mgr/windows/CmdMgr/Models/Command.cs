using System.ComponentModel;
using System.Runtime.CompilerServices;
using CmdMgr.ViewModels;

namespace CmdMgr.Models;

/// <summary>
/// A saved command configuration – matches the Python schema.
/// </summary>
public class Command : INotifyPropertyChanged
{
    public event PropertyChangedEventHandler? PropertyChanged;

    public int Id { get; set; }
    public string Name { get; set; } = "";
    public string CommandText { get; set; } = "";
    public string CmdType { get; set; } = "one-shot"; // "long-running" | "one-shot"
    public string? WorkingDirectory { get; set; }
    public DateTime CreatedAt { get; set; } = DateTime.Now;

    // Null until the command has been run at least once; then reflects the
    // outcome of its most recent run (Running while in flight).
    private ProcessStatus? _status;
    public ProcessStatus? Status
    {
        get => _status;
        set
        {
            if (_status == value) return;
            _status = value;
            OnPropertyChanged();
            OnPropertyChanged(nameof(IsRunning));
            OnPropertyChanged(nameof(HasRun));
        }
    }

    public bool IsRunning => Status == ProcessStatus.Running;

    // True once the process has completed (or started) at least one run.
    // Enables the "View Output" action even after the process has stopped.
    public bool HasRun => Status.HasValue;

    public bool IsLongRunning => CmdType == "long-running";
    public bool IsOneShot => CmdType == "one-shot";

    public string DisplayCommand => CommandText.Length > 80
        ? CommandText[..80] + "…"
        : CommandText;

    private void OnPropertyChanged([CallerMemberName] string? name = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));
}
