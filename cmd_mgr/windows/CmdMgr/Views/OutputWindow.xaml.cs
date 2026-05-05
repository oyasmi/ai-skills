using System.ComponentModel;
using System.Windows;
using CmdMgr.ViewModels;

namespace CmdMgr.Views;

public partial class OutputWindow : Window
{
    private readonly ProcessInfo _info;

    public OutputWindow(string commandName, ProcessInfo info)
    {
        InitializeComponent();
        _info = info;
        Title = $"Output: {commandName}";

        _info.PropertyChanged += OnInfoChanged;
        UpdateUI();
    }

    private void OnInfoChanged(object? sender, PropertyChangedEventArgs e)
    {
        if (e.PropertyName is not (nameof(ProcessInfo.Output) or nameof(ProcessInfo.IsRunning)))
            return;
        Dispatcher.Invoke(UpdateUI);
    }

    private void UpdateUI()
    {
        OutputBox.Text = _info.Output;
        OutputBox.ScrollToEnd();

        if (_info.IsRunning)
        {
            StatusDot.Fill = FindResource("SuccessBrush") as System.Windows.Media.SolidColorBrush;
            StatusText.Text = "Running";
        }
        else
        {
            StatusDot.Fill = FindResource("MutedBrush") as System.Windows.Media.SolidColorBrush;
            StatusText.Text = "Terminated";
        }
    }

    private void CopyAll_Click(object sender, RoutedEventArgs e)
    {
        if (!string.IsNullOrEmpty(_info.Output))
            System.Windows.Clipboard.SetText(_info.Output);
    }

    protected override void OnClosed(EventArgs e)
    {
        _info.PropertyChanged -= OnInfoChanged;
        base.OnClosed(e);
    }
}
