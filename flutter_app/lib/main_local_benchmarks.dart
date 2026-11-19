import 'package:flutter/material.dart';
import 'discovery/charts.dart';
import 'discovery/models.dart';
import 'discovery/widgets.dart';
import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_routing.dart' show taskLabel;
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
  final _auditKey = GlobalKey();
  Map<String, dynamic>? _data;
  String _task = '';
  String? _expanded;
  int _offset = 0;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load({String? task, int? offset}) async {
    if (_busy) return;
    final requestedTask = task ?? _task;
    var page = offset ?? _offset;
    try {
      setState(() => _busy = true);
      final api = widget.controller.api;
      var data = await api.internalBenchmarkResults(
        task: requestedTask,
        offset: page,
      );
      if (page > 0 && (data['results'] as List? ?? []).isEmpty) {
        page = 0;
        data = await api.internalBenchmarkResults(task: requestedTask);
      }
      if (mounted) {
        setState(() {
          _data = data;
          _task = data['task'] as String? ?? requestedTask;
          _offset = page;
          if (task != null || offset != null) _expanded = null;
        });
      }
    } catch (_) {
      // External benchmarks remain available if ReplayBench could not load.
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _toggle(ReplayBenchmarkResult model, {bool scroll = false}) {
    setState(() => _expanded = _expanded == model.id ? null : model.id);
    if (scroll && _expanded != null) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        final audit = _auditKey.currentContext;
        if (mounted && audit != null) Scrollable.ensureVisible(audit);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final data = _data;
    if (data == null) return const SizedBox.shrink();
    final results = (data['results'] as List? ?? [])
        .cast<Map>()
        .map(ReplayBenchmarkResult.new)
        .toList();
    if (results.isEmpty && data['enabled'] != true) {
      return const SizedBox.shrink();
    }
    final tasks =
        (data['tasks'] as List?)?.cast<String>() ??
        results.map((model) => model.task).toSet().toList();
    final task = tasks.contains(_task) ? _task : tasks.firstOrNull;
    final models = results.where((model) => model.task == task).toList();
    // Legacy servers return the whole model group in sample-count order.
    if (data['tasks'] == null) {
      models.sort((a, b) => b.score.compareTo(a.score));
    }
    final pageSize = data['limit'] as int? ?? 24;
    final total = data['total'] as int? ?? models.length;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SectionCard(
          title: t.lbTitle,
          trailing: IconButton(
            tooltip: t.refresh,
            onPressed: _busy ? null : () => _load(),
            icon: const Icon(Icons.refresh, size: 18),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                t.lbResultsSubtitle,
                style: Theme.of(context).textTheme.bodyLarge,
              ),
              const SizedBox(height: 8),
              Text(
                t.lbPreferenceHint,
                style: TextStyle(color: palette.textSoft, height: 1.4),
              ),
              if (_busy) const LinearProgressIndicator(minHeight: 2),
              if (models.isEmpty)
                Padding(
                  padding: const EdgeInsets.only(top: 16),
                  child: Text(t.lbNoEvidence('${data['minSamples']}')),
                )
              else ...[
                const SizedBox(height: 16),
                Wrap(
                  spacing: 10,
                  runSpacing: 10,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    SizedBox(
                      width: 220,
                      child: DropdownButtonFormField<String>(
                        key: ValueKey(task),
                        initialValue: task,
                        isDense: true,
                        isExpanded: true,
                        items: [
                          for (final item in tasks)
                            DropdownMenuItem(
                              value: item,
                              child: Text(taskLabel(t, item)),
                            ),
                        ],
                        onChanged: _busy
                            ? null
                            : (value) {
                                if (value != null) {
                                  _load(task: value, offset: 0);
                                }
                              },
                      ),
                    ),
                    Text(
                      t.discoveryPage('${_offset ~/ pageSize + 1}', '$total'),
                      style: TextStyle(color: palette.textMuted, fontSize: 12),
                    ),
                  ],
                ),
                const SizedBox(height: 16),
                Text(
                  t.lbPreferenceScale,
                  style: Theme.of(context).textTheme.labelMedium,
                ),
                const SizedBox(height: 8),
                ScoreBarChart<ReplayBenchmarkResult>(
                  models: models,
                  selected: {?_expanded},
                  onTap: (model) => _toggle(model, scroll: true),
                ),
              ],
            ],
          ),
        ),
        if (models.isNotEmpty) ...[
          const SizedBox(height: 16),
          SettingsCardList(
            children: [
              for (final (index, model) in models.indexed)
                Column(
                  key: ValueKey(model.id),
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    BenchmarkRankRow(
                      model: model,
                      rank: _offset + index + 1,
                      detail: t.lbEvaluatorComparisons('${model.samples}'),
                      percentage: true,
                      selected: _expanded == model.id,
                      onTap: () => _toggle(model),
                      trailing: IconButton(
                        visualDensity: VisualDensity.compact,
                        iconSize: 18,
                        tooltip: t.lbEvaluationDetails,
                        onPressed: () => _toggle(model),
                        icon: Icon(
                          _expanded == model.id
                              ? Icons.expand_less
                              : Icons.expand_more,
                        ),
                      ),
                    ),
                    if (_expanded == model.id)
                      Padding(
                        key: _auditKey,
                        padding: const EdgeInsets.fromLTRB(14, 0, 14, 12),
                        child: _EvaluationAudit(row: model.data),
                      ),
                  ],
                ),
            ],
          ),
        ],
        if (total > pageSize)
          BenchmarkPagination(
            offset: _offset,
            pageSize: pageSize,
            total: total,
            busy: _busy,
            onPage: (offset) => _load(offset: offset),
          ),
      ],
    );
  }
}

class _EvaluationAudit extends StatelessWidget {
  const _EvaluationAudit({required this.row});

  final Map row;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final theme = Theme.of(context).textTheme;
    final palette = neoRouterPaletteOf(context);
    final formatDate = MaterialLocalizations.of(context).formatMediumDate;
    final end = row['evaluatedAt'] as int;
    final start = row['evaluatedFrom'] as int? ?? end;
    final lastDay = formatDate(
      DateTime.fromMillisecondsSinceEpoch(end, isUtc: true),
    );
    final period = start == end
        ? t.lbEvidenceDate(lastDay)
        : t.lbEvidencePeriod(
            formatDate(DateTime.fromMillisecondsSinceEpoch(start, isUtc: true)),
            lastDay,
          );
    final evaluators = (row['evaluators'] as List? ?? []).cast<Map>();
    var judgeNumber = 0;
    return DecoratedBox(
      decoration: BoxDecoration(
        color: palette.surfaceRaised,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(t.lbEvaluationDetails, style: theme.titleSmall),
            const SizedBox(height: 4),
            Text(period, style: TextStyle(color: palette.textMuted)),
            const SizedBox(height: 8),
            for (final evaluator in evaluators)
              ListTile(
                contentPadding: EdgeInsets.zero,
                dense: true,
                title: Text(
                  evaluator['method'] == 'constraints' ||
                          evaluator['model'] == 'objective'
                      ? t.lbConstraintChecks
                      : t.lbJudgeNumber('${++judgeNumber}') +
                            (evaluator['name'] is String
                                ? ' · ${evaluator['name']}'
                                : ''),
                ),
                subtitle: Text(
                  [
                    if (evaluator['samples'] is int)
                      t.lbEvaluatorComparisons('${evaluator['samples']}'),
                    '${evaluator['version']}',
                  ].join(' · '),
                ),
                trailing: evaluator['score'] is num
                    ? Text(
                        '${((evaluator['score'] as num) * 100).round()}%',
                        style: theme.labelLarge,
                      )
                    : null,
              ),
            const SizedBox(height: 4),
            Text(
              t.lbAuditHint,
              style: theme.bodySmall?.copyWith(color: palette.textMuted),
            ),
          ],
        ),
      ),
    );
  }
}
