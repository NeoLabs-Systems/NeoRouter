import 'dart:async';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';
import '../l10n/gen/app_localizations.dart';
import '../main_controller.dart';
import '../main_local_benchmarks.dart';
import '../main_shared.dart';
import '../main_theme.dart';
import '../src/capabilities.dart';
import 'charts.dart';
import 'comparison.dart';
import 'models.dart';
import 'widgets.dart';

class BenchmarksScreen extends StatefulWidget {
  const BenchmarksScreen({super.key, required this.controller});
  final NeoRouterController controller;
  @override
  State<BenchmarksScreen> createState() => _BenchmarksScreenState();
}

class _BenchmarksScreenState extends State<BenchmarksScreen> {
  BenchmarkPageData? _data;
  final _search = TextEditingController();
  final _comparisonKey = GlobalKey();
  final List<BenchmarkModel> _selected = [];
  Timer? _debounce;
  String _capability = 'chat', _source = '';
  String? _error;
  bool _free = false, _busy = true, _valueChart = false;
  int _offset = 0, _request = 0;
  static const _pageSize = 24;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _search.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final request = ++_request;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final data = await widget.controller.api.benchmarks(
        query: _search.text,
        capability: _capability,
        source: _source,
        free: _free,
        offset: _offset,
        compare: _selected.map((model) => model.id).toList(),
      );
      if (!mounted || request != _request) return;
      setState(() {
        _data = data;
        for (var i = 0; i < _selected.length; i++) {
          _selected[i] =
              data.comparison
                  .where((model) => model.id == _selected[i].id)
                  .firstOrNull ??
              _selected[i];
        }
      });
    } catch (error) {
      if (mounted && request == _request) setState(() => _error = '$error');
    } finally {
      if (mounted && request == _request) setState(() => _busy = false);
    }
  }

  void _filter(VoidCallback update) {
    _debounce?.cancel();
    setState(() {
      update();
      _offset = 0;
    });
    _load();
  }

  void _choose(BenchmarkModel model) {
    setState(() {
      if (_selected.any((item) => item.id == model.id)) {
        _selected.removeWhere((item) => item.id == model.id);
      } else if (_selected.length < 2 &&
          (_selected.isEmpty ||
              _selected.first.capability == model.capability)) {
        _selected.add(model);
      }
    });
    if (_selected.length == 2) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        final context = _comparisonKey.currentContext;
        if (mounted && context != null) {
          Scrollable.ensureVisible(
            context,
            duration: MediaQuery.disableAnimationsOf(context)
                ? Duration.zero
                : const Duration(milliseconds: 300),
            curve: Curves.easeOutCubic,
          );
        }
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final data = _data;
    final capabilities = <String>{_capability, ...?data?.capabilities};
    final selectedIds = _selected.map((model) => model.id).toSet();
    bool canSelect(BenchmarkModel model) =>
        selectedIds.contains(model.id) ||
        (_selected.length < 2 &&
            (_selected.isEmpty ||
                _selected.first.capability == model.capability));
    return PageScaffold(
      header: ScreenHeader(
        title: t.discoveryBenchmarks,
        description: '',
        trailing: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Tooltip(
              message: t.discoveryMethodHint,
              child: Icon(
                Icons.info_outline,
                size: 18,
                color: palette.textMuted,
              ),
            ),
            IconButton(
              tooltip: t.refresh,
              onPressed: _busy ? null : _load,
              icon: const Icon(Icons.refresh),
            ),
          ],
        ),
      ),
      children: [
        Wrap(
          spacing: 10,
          runSpacing: 10,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            SizedBox(
              width: 260,
              child: TextField(
                controller: _search,
                decoration: InputDecoration(
                  isDense: true,
                  hintText: t.discoverySearchModels,
                  prefixIcon: const Icon(Icons.search, size: 18),
                ),
                onChanged: (_) {
                  _debounce?.cancel();
                  // Invalidate an older request immediately, even during the debounce.
                  _request++;
                  _debounce = Timer(
                    const Duration(milliseconds: 250),
                    () => _filter(() {}),
                  );
                },
              ),
            ),
            SizedBox(
              width: 170,
              child: DropdownButtonFormField<String>(
                initialValue: _capability,
                isDense: true,
                isExpanded: true,
                items: [
                  for (final kind in capabilities)
                    DropdownMenuItem(
                      value: kind,
                      child: Text(capabilityInfo(kind).label(t)),
                    ),
                ],
                onChanged: (value) {
                  if (value != null) _filter(() => _capability = value);
                },
              ),
            ),
            SizedBox(
              width: 220,
              child: DropdownButtonFormField<String>(
                initialValue: _source,
                isDense: true,
                isExpanded: true,
                items: [
                  DropdownMenuItem(
                    value: '',
                    child: Text(t.discoveryAllSources),
                  ),
                  for (final source in data?.sources ?? <BenchmarkSource>[])
                    DropdownMenuItem(
                      value: source.id,
                      child: Text(source.name, overflow: TextOverflow.ellipsis),
                    ),
                ],
                onChanged: (value) {
                  if (value != null) _filter(() => _source = value);
                },
              ),
            ),
            Tooltip(
              message: t.discoveryFreeHint,
              child: FilterChip(
                label: Text(t.discoveryFreeOnly),
                selected: _free,
                onSelected: (value) => _filter(() => _free = value),
              ),
            ),
            if (data != null)
              Text(
                t.discoveryPage('${_offset ~/ _pageSize + 1}', '${data.total}'),
                style: TextStyle(color: palette.textMuted, fontSize: 12),
              ),
          ],
        ),
        if (_busy) const LinearProgressIndicator(minHeight: 2),
        if (_error != null && data != null) InlineError(message: _error!),
        AsyncBody(
          loading: data == null && _busy,
          error: data == null ? _error : null,
          onRetry: _load,
          child: data == null
              ? const SizedBox.shrink()
              : data.models.isEmpty
              ? AppCard(
                  child: EmptyState(
                    icon: Icons.query_stats,
                    title: t.discoveryEmpty,
                    message: t.discoveryEmptyHint,
                  ),
                )
              : Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    DiscoveryReveal(
                      child: AppCard(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: [
                            Align(
                              alignment: Alignment.centerLeft,
                              child: SegmentedButton<bool>(
                                showSelectedIcon: false,
                                segments: [
                                  ButtonSegment(
                                    value: false,
                                    label: Text(t.discoveryChartScore),
                                  ),
                                  ButtonSegment(
                                    value: true,
                                    label: Text(t.discoveryChartValue),
                                  ),
                                ],
                                selected: {_valueChart},
                                onSelectionChanged: (values) =>
                                    setState(() => _valueChart = values.first),
                              ),
                            ),
                            const SizedBox(height: 16),
                            _valueChart
                                ? ScoreCostChart(models: data.models)
                                : ScoreBarChart(
                                    models: data.models,
                                    selected: selectedIds,
                                    onTap: (model) {
                                      if (canSelect(model)) _choose(model);
                                    },
                                  ),
                          ],
                        ),
                      ),
                    ),
                    if (_selected.isNotEmpty) ...[
                      const SizedBox(height: 16),
                      BenchmarkComparison(
                        key: _comparisonKey,
                        models: _selected,
                        sources: data.sources,
                        onRemove: _choose,
                      ),
                    ],
                    const SizedBox(height: 16),
                    SettingsCardList(
                      children: [
                        for (var i = 0; i < data.models.length; i++)
                          _BenchmarkRow(
                            key: ValueKey(data.models[i].id),
                            model: data.models[i],
                            rank: _offset + i + 1,
                            selected: selectedIds.contains(data.models[i].id),
                            canSelect: canSelect(data.models[i]),
                            onSelect: () => _choose(data.models[i]),
                          ),
                      ],
                    ),
                  ],
                ),
        ),
        if (data != null && data.total > _pageSize)
          Row(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              IconButton(
                tooltip: t.discoveryPrevious,
                onPressed: _busy || _offset == 0
                    ? null
                    : () {
                        _offset = (_offset - _pageSize).clamp(0, data.total);
                        _load();
                      },
                icon: const Icon(Icons.chevron_left),
              ),
              IconButton(
                tooltip: t.discoveryNext,
                onPressed: _busy || _offset + _pageSize >= data.total
                    ? null
                    : () {
                        _offset += _pageSize;
                        _load();
                      },
                icon: const Icon(Icons.chevron_right),
              ),
            ],
          ),
        if (data != null)
          Wrap(
            spacing: 8,
            runSpacing: 8,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              Text(
                t.discoverySources,
                style: TextStyle(color: palette.textMuted, fontSize: 12),
              ),
              for (final source in data.sources)
                ActionChip(
                  visualDensity: VisualDensity.compact,
                  label: Text(
                    source.name,
                    style: const TextStyle(fontSize: 12),
                  ),
                  tooltip: source.updatedAt == null
                      ? t.discoveryNotSynced
                      : t.discoveryUpdated(
                          discoveryDate(context, source.updatedAt!),
                        ),
                  onPressed: () => launchUrl(
                    source.url,
                    mode: LaunchMode.externalApplication,
                  ),
                ),
            ],
          ),
        LocalBenchmarkResults(controller: widget.controller),
      ],
    );
  }
}

class _BenchmarkRow extends StatelessWidget {
  const _BenchmarkRow({
    super.key,
    required this.model,
    required this.rank,
    required this.selected,
    required this.canSelect,
    required this.onSelect,
  });
  final BenchmarkModel model;
  final int rank;
  final bool selected, canSelect;
  final VoidCallback onSelect;
  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return InkWell(
      onTap: canSelect ? onSelect : null,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
        child: Row(
          children: [
            SizedBox(
              width: 28,
              child: Text(
                '$rank',
                style: TextStyle(color: palette.textMuted, fontSize: 12),
              ),
            ),
            Expanded(
              flex: 3,
              child: Row(
                children: [
                  Flexible(
                    child: Text(
                      model.name,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(color: palette.text, fontSize: 13),
                    ),
                  ),
                  if (model.isFree) ...[
                    const SizedBox(width: 6),
                    Tooltip(
                      message: t.discoveryFree,
                      child: Icon(Icons.circle, size: 7, color: palette.accent),
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              flex: 2,
              child: Semantics(
                label:
                    '${t.discoveryScore}: ${model.score.toStringAsFixed(1)} / 100',
                child: BenchmarkScoreBar(
                  score: model.score,
                  selected: selected,
                ),
              ),
            ),
            SizedBox(
              width: 44,
              child: Text(
                model.score.toStringAsFixed(1),
                textAlign: TextAlign.right,
                style: TextStyle(
                  color: selected ? palette.accent : palette.text,
                  fontWeight: FontWeight.w500,
                  fontSize: 13,
                ),
              ),
            ),
            IconButton(
              visualDensity: VisualDensity.compact,
              iconSize: 18,
              tooltip: canSelect
                  ? selected
                        ? t.discoveryRemoveCompare(model.name)
                        : t.discoverySelectCompare(model.name)
                  : t.discoveryCompareLimit,
              onPressed: canSelect ? onSelect : null,
              icon: Icon(
                selected ? Icons.check_circle : Icons.add_circle_outline,
                color: selected ? palette.accent : palette.textMuted,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
