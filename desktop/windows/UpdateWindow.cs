namespace Cage.Windows;

// The window of an update in progress: the versions, then one row per step
// with its state, and the result when it fails. Names and states only.
internal sealed class UpdateWindow : Form
{
    private readonly List<(Label Mark, Label Name, ProgressBar Bar)> steps = new();
    private readonly Label status = new() { AutoSize = true, Visible = false, Margin = new Padding(0, 12, 0, 0) };
    private readonly ToolTip tip = new();
    private readonly FlowLayoutPanel actions = new() { AutoSize = true, FlowDirection = FlowDirection.RightToLeft, Anchor = AnchorStyles.Right, Visible = false, Margin = new Padding(0, 16, 0, 0) };

    // False while an update runs: closing then only minimizes, since a start
    // of the App brings this window back rather than starting.
    [System.ComponentModel.DesignerSerializationVisibility(System.ComponentModel.DesignerSerializationVisibility.Hidden)]
    public bool Closable { get; set; } = true;

    public UpdateWindow(string? versions, IReadOnlyList<string> stepNames)
    {
        FormClosing += (_, e) =>
        {
            if (Closable || e.CloseReason != CloseReason.UserClosing) return;
            e.Cancel = true; WindowState = FormWindowState.Minimized;
        };
        Text = InterfaceLanguage.Text("Cage lite 更新");
        Icon = Icon.ExtractAssociatedIcon(Environment.ProcessPath!) ?? SystemIcons.Application;
        AutoScaleMode = AutoScaleMode.Dpi; AutoScaleDimensions = new SizeF(96, 96);
        Font = SystemFonts.MessageBoxFont ?? Font;
        FormBorderStyle = FormBorderStyle.FixedSingle; MaximizeBox = false;
        StartPosition = FormStartPosition.CenterScreen;
        AutoSize = true; AutoSizeMode = AutoSizeMode.GrowAndShrink;
        MinimumSize = new Size(360, 0);
        var layout = new TableLayoutPanel { AutoSize = true, AutoSizeMode = AutoSizeMode.GrowAndShrink, ColumnCount = 3, Padding = new Padding(24, 20, 24, 20), Location = Point.Empty };
        layout.ColumnStyles.Add(new ColumnStyle(SizeType.AutoSize));
        layout.ColumnStyles.Add(new ColumnStyle(SizeType.AutoSize));
        // The bar's column keeps its width while no bar shows.
        layout.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 170));
        var rows = 0;
        void Line(params (Control Control, int Span)[] cells)
        {
            var column = 0;
            foreach (var (control, span) in cells)
            {
                layout.Controls.Add(control, column, rows);
                if (span > 1) layout.SetColumnSpan(control, span);
                column += span;
            }
            layout.RowStyles.Add(new RowStyle(SizeType.AutoSize));
            layout.RowCount = ++rows;
        }
        if (versions != null)
            Line((new Label { Text = versions, AutoSize = true, Font = new Font(Font.FontFamily, Font.Size * 1.3f, FontStyle.Bold), Margin = new Padding(0, 0, 0, 14) }, 3));
        foreach (var name in stepNames)
        {
            var row = (Mark: new Label { AutoSize = true, Margin = new Padding(0, 5, 8, 5) },
                Name: new Label { Text = InterfaceLanguage.Text(name), AutoSize = true, Margin = new Padding(0, 5, 20, 5) },
                Bar: new ProgressBar { Width = 160, Height = 10, Visible = false, Anchor = AnchorStyles.Left, Margin = new Padding(0, 8, 0, 5) });
            Line((row.Mark, 1), (row.Name, 1), (row.Bar, 1));
            steps.Add(row);
        }
        Line((status, 3));
        Line((actions, 3));
        Controls.Add(layout);
        SetStep(-1, null);
    }

    // Steps before index are done; index runs, with its percent or, unknown,
    // a moving bar; the rest wait.
    public void SetStep(int index, int? percent)
    {
        for (var i = 0; i < steps.Count; i++)
        {
            var (mark, name, bar) = steps[i];
            mark.Text = i < index ? "✓" : i == index ? "●" : "○";
            mark.ForeColor = i <= index ? SystemColors.ControlText : SystemColors.GrayText;
            name.ForeColor = mark.ForeColor;
            bar.Visible = i == index;
            if (i != index) continue;
            bar.Style = percent == null ? ProgressBarStyle.Marquee : ProgressBarStyle.Continuous;
            if (percent is { } value) bar.Value = Math.Clamp(value, 0, 100);
        }
    }

    public void SetStatus(string text)
    {
        status.Text = text; status.Visible = text.Length > 0;
    }

    // The full message behind the result, for whoever diagnoses it.
    public void SetDetail(string? detail) => tip.SetToolTip(status, detail ?? "");

    // The step at index failed: the result line and what can be done next.
    public void Fail(int index, string result, params (string Text, Action<Button> Click)[] next)
    {
        SetStep(index, 0);
        if (index >= 0 && index < steps.Count)
        {
            steps[index].Mark.Text = "✗";
            steps[index].Bar.Visible = false;
        }
        SetStatus(result);
        actions.Controls.Clear();
        // Right to left: the first action is the rightmost, the default one.
        Closable = true;
        foreach (var (text, click) in next)
        {
            var button = new Button { Text = InterfaceLanguage.Text(text), AutoSize = true, Margin = new Padding(8, 0, 0, 0) };
            button.Click += (_, _) => click(button);
            actions.Controls.Add(button);
        }
        actions.Visible = next.Length > 0;
        if (actions.Controls.Count > 0) AcceptButton = (IButtonControl)actions.Controls[0];
    }
}
