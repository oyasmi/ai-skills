namespace CmdMgr.Models;

/// <summary>
/// A record of a command execution.
/// </summary>
public class CommandHistory
{
    public int Id { get; set; }
    public int CommandId { get; set; }
    public DateTime StartTime { get; set; }
    public DateTime? EndTime { get; set; }
    public string Status { get; set; } = "";
    public string? Output { get; set; }

    /// <summary>Human-readable run duration, e.g. "2m 34s". Null while still running.</summary>
    public string? Duration
    {
        get
        {
            if (EndTime is not { } end) return null;
            var secs = (int)(end - StartTime).TotalSeconds;
            if (secs < 60) return $"{secs}s";
            var m = secs / 60; var s = secs % 60;
            if (m < 60) return $"{m}m {s}s";
            var h = m / 60; var rm = m % 60;
            return $"{h}h {rm}m";
        }
    }
}
