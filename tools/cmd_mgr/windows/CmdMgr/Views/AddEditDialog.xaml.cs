using System.IO;
using System.Windows;
using System.Windows.Forms;
using CmdMgr.Models;

namespace CmdMgr.Views;

public partial class AddEditDialog : Window
{
    private readonly Command? _existing;
    private bool _isEdit;

    public string CommandName => NameBox.Text.Trim();
    public string CommandText => CommandBox.Text.Trim();
    public string CommandType => LongRunningRadio.IsChecked == true ? "long-running" : "one-shot";
    public string? WorkingDirectory
    {
        get
        {
            var s = WorkingDirBox.Text.Trim();
            return string.IsNullOrEmpty(s) ? null : s;
        }
    }

    public AddEditDialog(Command? existing = null)
    {
        InitializeComponent();
        _existing = existing;
        _isEdit = existing != null;

        if (existing != null)
        {
            Title = "Edit Command";
            DialogTitle.Text = "Edit Command";
            SaveButton.Content = "Save Changes";
            NameBox.Text = existing.Name;
            CommandBox.Text = existing.CommandText;
            WorkingDirBox.Text = existing.WorkingDirectory ?? "";
            LongRunningRadio.IsChecked = existing.IsLongRunning;
            OneShotRadio.IsChecked = existing.IsOneShot;
        }

        UpdateValidationState();
        Loaded += (_, _) => NameBox.Focus();
    }

    private void TypeRadio_Checked(object sender, RoutedEventArgs e)
    {
        if (TypeHint == null) return;
        TypeHint.Text = LongRunningRadio.IsChecked == true
            ? "For servers and background processes. The command keeps running until you stop it."
            : "For scripts and tasks that finish on their own.";
    }

    private void Field_Changed(object sender, RoutedEventArgs e) => UpdateValidationState();

    private string? DirectoryError()
    {
        var dir = WorkingDirBox.Text.Trim();
        if (string.IsNullOrEmpty(dir)) return null;
        return Directory.Exists(dir) ? null : "Choose an existing directory.";
    }

    private void UpdateValidationState()
    {
        var hasRequiredFields = !string.IsNullOrWhiteSpace(NameBox.Text)
            && !string.IsNullOrWhiteSpace(CommandBox.Text);
        var directoryError = DirectoryError();

        if (directoryError != null)
        {
            DirectoryHint.Text = directoryError;
            DirectoryHint.Foreground = (System.Windows.Media.Brush)FindResource("DangerBrush");
        }
        else
        {
            DirectoryHint.Text = "Leave empty to inherit the app's working directory.";
            DirectoryHint.Foreground = (System.Windows.Media.Brush)FindResource("MutedBrush");
        }

        ValidationHint.Visibility = hasRequiredFields ? Visibility.Hidden : Visibility.Visible;
        SaveButton.IsEnabled = hasRequiredFields && directoryError == null;
    }

    private void Browse_Click(object sender, RoutedEventArgs e)
    {
        using var dlg = new FolderBrowserDialog
        {
            Description = "Select working directory",
            UseDescriptionForTitle = true,
            SelectedPath = WorkingDirBox.Text.Trim()
        };
        if (dlg.ShowDialog() == System.Windows.Forms.DialogResult.OK)
            WorkingDirBox.Text = dlg.SelectedPath;
    }

    private void Save_Click(object sender, RoutedEventArgs e)
    {
        UpdateValidationState();
        if (!SaveButton.IsEnabled) return;
        DialogResult = true;
    }

    private void Cancel_Click(object sender, RoutedEventArgs e) => DialogResult = false;
}
