using System.Text;
using System.Windows;

namespace CmdMgr;

public partial class App : System.Windows.Application
{
    protected override void OnStartup(StartupEventArgs e)
    {
        // Required for .NET Core/5+: register system code pages (e.g. GBK/936)
        // so that sub-process output encoding works on non-UTF8 Windows locales.
        Encoding.RegisterProvider(CodePagesEncodingProvider.Instance);
        base.OnStartup(e);
    }
}
