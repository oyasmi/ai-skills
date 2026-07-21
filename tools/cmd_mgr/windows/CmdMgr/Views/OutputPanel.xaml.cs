using System.ComponentModel;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Threading;
using CmdMgr.ViewModels;

namespace CmdMgr.Views;

/// <summary>
/// Shows the live (or last completed) output for a command's process.
/// Not bound via a plain DataContext because it needs to attach/detach
/// PropertyChanged handlers as the selected command's process comes and goes.
/// </summary>
public partial class OutputPanel : UserControl
{
    private ProcessInfo? _info;
    private Action? _runAction;
    private DispatcherTimer? _copyResetTimer;

    public OutputPanel()
    {
        InitializeComponent();
    }

    public void Attach(ProcessInfo? info, Action runAction)
    {
        if (_info != null)
            _info.PropertyChanged -= OnInfoChanged;

        _info = info;
        _runAction = runAction;

        if (_info != null)
            _info.PropertyChanged += OnInfoChanged;

        Refresh();
    }

    private void OnInfoChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (e.PropertyName is not (nameof(ProcessInfo.Output) or nameof(ProcessInfo.Status)))
            return;
        Dispatcher.Invoke(Refresh);
    }

    private void Refresh()
    {
        if (_info == null)
        {
            EmptyState.Visibility = Visibility.Visible;
            ConsoleRoot.Visibility = Visibility.Collapsed;
            return;
        }

        EmptyState.Visibility = Visibility.Collapsed;
        ConsoleRoot.Visibility = Visibility.Visible;

        StatusIcon.Text = _info.Status.Icon();
        StatusText.Text = _info.Status.DisplayName();
        var brush = (Brush)TryFindResource(_info.Status.BrushKey());
        StatusIcon.Foreground = brush;
        StatusText.Foreground = brush;

        var wasNearBottom = IsNearBottom();
        if (OutputBox.Text != _info.Output)
            OutputBox.Text = _info.Output;

        var hasOutput = !string.IsNullOrEmpty(_info.Output);
        WaitingText.Visibility = hasOutput ? Visibility.Collapsed : Visibility.Visible;
        WaitingText.Text = _info.IsRunning ? "Waiting for output…" : "No output was captured.";
        OutputBox.Visibility = hasOutput ? Visibility.Visible : Visibility.Collapsed;
        CopyAllButton.IsEnabled = hasOutput;

        if (hasOutput && (FollowCheck.IsChecked == true || wasNearBottom))
            OutputBox.ScrollToEnd();
    }

    private bool IsNearBottom()
    {
        if (OutputBox.ExtentHeight <= 0) return true;
        return OutputBox.ExtentHeight - OutputBox.VerticalOffset - OutputBox.ViewportHeight < 40;
    }

    private void OutputBox_TextChanged(object sender, TextChangedEventArgs e)
    {
        if (FollowCheck.IsChecked == true)
            OutputBox.ScrollToEnd();
    }

    private void WrapCheck_Changed(object sender, RoutedEventArgs e)
    {
        OutputBox.TextWrapping = WrapCheck.IsChecked == true
            ? TextWrapping.Wrap
            : TextWrapping.NoWrap;
    }

    private void RunButton_Click(object sender, RoutedEventArgs e) => _runAction?.Invoke();

    private void CopyAll_Click(object sender, RoutedEventArgs e)
    {
        if (_info == null || string.IsNullOrEmpty(_info.Output)) return;
        System.Windows.Clipboard.SetText(_info.Output);

        CopyAllButton.Content = "Copied";
        _copyResetTimer?.Stop();
        _copyResetTimer = new DispatcherTimer { Interval = TimeSpan.FromSeconds(1.5) };
        _copyResetTimer.Tick += (_, _) =>
        {
            CopyAllButton.Content = "Copy All";
            _copyResetTimer?.Stop();
        };
        _copyResetTimer.Start();
    }
}
