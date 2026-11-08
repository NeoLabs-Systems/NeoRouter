import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';

/// Settings → Usage → even out usage. Locked, with what is still missing,
/// until NeoRouter has learned enough; then a plain switch.
class SmoothingSection extends StatefulWidget {
  const SmoothingSection({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<SmoothingSection> createState() => _SmoothingSectionState();
}

class _SmoothingSectionState extends State<SmoothingSection> {
  Map<String, dynamic>? _status;
  String? _error;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final status = await widget.controller.api.smoothing();
      if (mounted) setState(() => _status = status);
    } on ApiException catch (error) {
      if (mounted) setState(() => _error = error.message);
    }
  }

  Future<void> _set(bool enabled) async {
    setState(() => _saving = true);
    try {
      final status = await widget.controller.api.setSmoothing(enabled);
      if (mounted) setState(() => _status = status);
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final status = _status;
    return AsyncBody(
      loading: status == null && _error == null,
      error: status == null ? _error : null,
      onRetry: _load,
      child: status == null ? const SizedBox() : _content(context, status),
    );
  }

  Widget _content(BuildContext context, Map<String, dynamic> status) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final readiness = Map<String, dynamic>.from((status['readiness'] as Map?) ?? const <String, dynamic>{});
    final needed = Map<String, dynamic>.from((readiness['needed'] as Map?) ?? const <String, dynamic>{});
    final ready = readiness['ready'] == true;
    final enabled = status['enabled'] == true;
    final active = status['active'] == true;
    final mayEdit = widget.controller.can('routing.edit');
    // Locked until there is enough data; one already on can always be turned off.
    final canToggle = mayEdit && (ready || enabled) && !_saving;
    final (pill, tone) = active
        ? (t.smoothStatusActive, PillTone.success)
        : !ready
        ? (t.smoothStatusLearning, PillTone.neutral)
        : (t.smoothStatusOff, PillTone.neutral);
    final description = active ? t.smoothOn : enabled ? t.smoothPausedLearning : ready ? t.smoothOff : t.smoothLearning;
    num value(String key) => (readiness[key] as num?) ?? 0;
    num need(String key) => (needed[key] as num?) ?? 1;
    final hint = TextStyle(color: palette.textSoft, fontSize: 13, height: 1.45);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsHeading(
          title: t.smoothTitle,
          description: t.smoothHint,
          trailing: MetaPill(label: pill, tone: tone, icon: active ? Icons.waves_rounded : !ready ? Icons.hourglass_bottom_rounded : null),
        ),
        SettingsCardList(
          children: <Widget>[
            SettingsRow(
              label: t.smoothSwitch,
              description: description,
              control: _saving
                  ? const SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2))
                  : Switch(value: enabled, onChanged: canToggle ? _set : null),
            ),
            // What is still missing, as progress toward each threshold.
            if (!ready)
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 4, 20, 20),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: <Widget>[
                    _Need(label: t.smoothNeedDays, have: value('days'), need: need('days'), text: t.smoothProgress('${value('days')}', '${need('days')}')),
                    _Need(label: t.smoothNeedActiveDays, have: value('activeDays'), need: need('activeDays'), text: t.smoothProgress('${value('activeDays')}', '${need('activeDays')}')),
                    _Need(label: t.smoothNeedRequests, have: value('requests'), need: need('requests'), text: t.smoothProgress('${value('requests')}', '${need('requests')}')),
                    _Need(
                      label: t.smoothNeedCoverage,
                      have: value('coverage'),
                      need: need('coverage'),
                      text: t.smoothProgress('${(value('coverage') * 100).round()} %', '${(need('coverage') * 100).round()} %'),
                    ),
                  ],
                ),
              ),
          ],
        ),
        const SizedBox(height: 28),
        SettingsHeading(title: t.smoothHowTitle),
        SettingsCardList(
          children: <Widget>[
            Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  for (final (icon, text) in <(IconData, String)>[
                    (Icons.schedule_rounded, t.smoothHowDaily),
                    (Icons.event_repeat_rounded, t.smoothHowPlans),
                    (Icons.bolt_rounded, t.smoothHowPriority),
                  ])
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 6),
                      child: Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: <Widget>[
                          Icon(icon, size: 17, color: palette.textMuted),
                          const SizedBox(width: AppSpacing.sm),
                          Expanded(child: Text(text, style: hint)),
                        ],
                      ),
                    ),
                ],
              ),
            ),
          ],
        ),
      ],
    );
  }
}

/// One threshold: what it counts, how far along, and a bar.
class _Need extends StatelessWidget {
  const _Need({required this.label, required this.have, required this.need, required this.text});

  final String label;
  final num have;
  final num need;
  final String text;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final done = have >= need;
    return Padding(
      padding: const EdgeInsets.only(top: 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(child: Text(label, style: TextStyle(fontSize: 13, color: palette.textSoft))),
              if (done) ...<Widget>[Icon(Icons.check_rounded, size: 15, color: palette.success), const SizedBox(width: 4)],
              Text(text, style: TextStyle(fontSize: 13, color: done ? palette.text : palette.textMuted)),
            ],
          ),
          const SizedBox(height: 6),
          UsageBar(fraction: need == 0 ? 1 : have / need, height: 4),
        ],
      ),
    );
  }
}
