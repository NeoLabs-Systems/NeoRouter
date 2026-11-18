import 'package:flutter/material.dart';
import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_theme.dart';

/// Account consent and private-data controls. Never renders sample contents.
class LocalBenchmarkSettings extends StatefulWidget {
  const LocalBenchmarkSettings({super.key, required this.controller});
  final NeoRouterController controller;
  @override
  State<LocalBenchmarkSettings> createState() => _LocalBenchmarkSettingsState();
}

class _LocalBenchmarkSettingsState extends State<LocalBenchmarkSettings> {
  Map<String, dynamic>? _data;
  bool _busy = false;
  String? _error;
  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final data = await widget.controller.api.benchmarkConsent();
      if (mounted) setState(() => _data = data);
    } catch (error) {
      if (mounted) setState(() => _error = '$error');
    }
  }

  Future<void> _save(bool enabled) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final data = await widget.controller.api.setBenchmarkConsent(enabled);
      if (mounted) setState(() => _data = data);
    } catch (error) {
      if (mounted) setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final data = _data;
    final counts = (data?['counts'] as Map?) ?? {};
    final pending =
        (counts['pending'] as int? ?? 0) + (counts['running'] as int? ?? 0);
    final contributions = data?['contributions'] as int? ?? 0;
    if (data == null ||
        (data['adminEnabled'] != true && pending == 0 && contributions == 0)) {
      return const SizedBox.shrink();
    }
    return SettingsGroup(
      title: t.lbTitle,
      description: t.lbAccountHint,
      children: [
        if (data['adminEnabled'] == true)
          SettingsRow(
            label: t.lbUserEnable,
            description: data['enabled'] == true ? t.lbKeyNext : null,
            control: Semantics(
              label: t.lbUserEnable,
              hint: t.lbAccountHint,
              child: Switch(
                value: data['enabled'] == true,
                onChanged: _busy ? null : _save,
              ),
            ),
          ),
        SettingsRow(
          label: t.lbPrivateCount('$pending'),
          description: t.lbContributions('$contributions'),
          control: OutlinedButton(
            onPressed: _busy
                ? null
                : () async {
                    if (!await confirmAction(
                          context,
                          title: t.lbDelete,
                          message: t.lbDeleteConfirm,
                          confirmLabel: t.delete,
                          cancelLabel: t.cancel,
                        ) ||
                        !mounted) {
                      return;
                    }
                    setState(() {
                      _busy = true;
                      _error = null;
                    });
                    try {
                      await widget.controller.api.deleteBenchmarkCollection();
                      await _load();
                    } catch (error) {
                      if (mounted) setState(() => _error = '$error');
                    } finally {
                      if (mounted) setState(() => _busy = false);
                    }
                  },
            child: Text(t.lbDelete),
          ),
        ),
        if (_error != null) InlineError(message: _error!),
      ],
    );
  }
}

/// Lives inside the existing admin Server tab; no quota or concurrency knobs.
class LocalBenchmarkAdmin extends StatelessWidget {
  const LocalBenchmarkAdmin({
    super.key,
    required this.health,
    required this.onChanged,
    required this.onRefresh,
  });
  final Map<String, dynamic> health;
  final ValueChanged<bool> onChanged;
  final VoidCallback onRefresh;
  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final code = health['lastCode'];
    final status = health['active'] == true
        ? t.lbWorking
        : switch (code) {
            'waiting_free_models' ||
            'waiting_calibrated_judge' => t.lbWaitingModels,
            'waiting_capacity' => t.lbWaitingQuota,
            'network_unavailable' => t.lbRetrying,
            'provider_failure' ||
            'provider_timeout' ||
            'judge_invalid' ||
            'evaluation_failed' => t.lbRetrying,
            _ => t.lbIdle,
          };
    final counts = (health['counts'] as Map?) ?? {};
    return SettingsGroup(
      title: t.lbTitle,
      description: t.lbAdminHint,
      children: [
        SettingsRow(
          label: t.lbAdminEnable,
          control: Semantics(
            label: t.lbAdminEnable,
            child: Switch(
              value: health['enabled'] == true,
              onChanged: onChanged,
            ),
          ),
        ),
        if (health['enabled'] == true)
          SettingsRow(
            label: t.lbWorker,
            description: t.lbHealth(
              '${counts['pending'] ?? 0}',
              '${counts['validated'] ?? 0}',
              '${health['aggregates'] ?? 0}',
            ),
            control: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Flexible(child: Text(status)),
                IconButton(
                  tooltip: t.refresh,
                  onPressed: onRefresh,
                  icon: const Icon(Icons.refresh, size: 18),
                ),
              ],
            ),
          ),
      ],
    );
  }
}

/// Global metrics contain no user IDs, task text, responses or per-user hashes.
class LocalBenchmarkResults extends StatefulWidget {
  const LocalBenchmarkResults({super.key, required this.controller});
  final NeoRouterController controller;
  @override
  State<LocalBenchmarkResults> createState() => _LocalBenchmarkResultsState();
}

class _LocalBenchmarkResultsState extends State<LocalBenchmarkResults> {
  Map<String, dynamic>? _data;
  int _offset = 0;
  bool _busy = false;
  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    if (_busy) return;
    try {
      setState(() => _busy = true);
      var data = await widget.controller.api.internalBenchmarkResults(
        offset: _offset,
      );
      if (_offset > 0 && (data['results'] as List? ?? []).isEmpty) {
        _offset = 0;
        data = await widget.controller.api.internalBenchmarkResults();
      }
      if (mounted) setState(() => _data = data);
    } catch (_) {
      /* External benchmark browsing remains available. */
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  String _evaluators(Map row) => ((row['evaluators'] as List?) ?? [])
      .cast<Map>()
      .map((item) => "${item['model']} · ${item['version']}")
      .join(', ');

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final data = _data;
    if (data == null) return const SizedBox.shrink();
    final results = (data['results'] as List? ?? []).cast<Map>();
    if (results.isEmpty && data['enabled'] != true) {
      return const SizedBox.shrink();
    }
    return SectionCard(
      title: t.lbResults,
      trailing: IconButton(
        tooltip: t.refresh,
        onPressed: _busy ? null : _load,
        icon: const Icon(Icons.refresh, size: 18),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            t.lbEvidenceHint,
            style: TextStyle(color: palette.textSoft, height: 1.5),
          ),
          const SizedBox(height: 12),
          Text(
            t.lbPreferenceHint,
            style: TextStyle(color: palette.textMuted, fontSize: 12),
          ),
          if (results.isEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 16),
              child: Text(t.lbNoEvidence('${data['minSamples']}')),
            ),
          for (final row in results)
            ListTile(
              contentPadding: EdgeInsets.zero,
              title: Text('${row['name'] ?? row['model']}'),
              subtitle: Text(
                '${t.lbResultMeta('${row['task']}', '${row['samples']}', '${((row['confidence'] as num) * 100).round()}')}\n${t.lbRubric(_evaluators(row), formatTime(DateTime.fromMillisecondsSinceEpoch(row['evaluatedAt'] as int)))}',
              ),
              trailing: Tooltip(
                message: t.lbPreference(
                  '${((row['score'] as num) * 100).round()}',
                ),
                child: Text(
                  '${((row['score'] as num) * 100).round()}%',
                  style: TextStyle(
                    color: palette.text,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
            ),
          if ((data['total'] as int? ?? results.length) >
              (data['limit'] as int? ?? 24))
            Row(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                IconButton(
                  tooltip: t.discoveryPrevious,
                  onPressed: _busy || _offset == 0
                      ? null
                      : () {
                          _offset = (_offset - (data['limit'] as int? ?? 24))
                              .clamp(0, _offset);
                          _load();
                        },
                  icon: const Icon(Icons.chevron_left),
                ),
                Text('${_offset ~/ (data['limit'] as int? ?? 24) + 1}'),
                IconButton(
                  tooltip: t.discoveryNext,
                  onPressed:
                      _busy ||
                          _offset + results.length >=
                              (data['total'] as int? ?? results.length)
                      ? null
                      : () {
                          _offset += (data['limit'] as int? ?? 24);
                          _load();
                        },
                  icon: const Icon(Icons.chevron_right),
                ),
              ],
            ),
        ],
      ),
    );
  }
}
