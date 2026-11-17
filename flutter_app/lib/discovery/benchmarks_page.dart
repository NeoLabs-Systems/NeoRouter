import 'dart:async';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';
import '../l10n/gen/app_localizations.dart';
import '../main_controller.dart';
import '../main_shared.dart';
import '../main_theme.dart';
import '../src/capabilities.dart';
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
  bool _free = false, _busy = true;
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
    return PageScaffold(
      header: ScreenHeader(
        title: t.discoveryBenchmarks,
        description: t.discoveryBenchHint,
        trailing: IconButton(
          tooltip: t.refresh,
          onPressed: _busy ? null : _load,
          icon: const Icon(Icons.refresh),
        ),
      ),
      children: [
        if (data != null)
          DiscoveryReveal(
            child: MetricStrip(
              metrics: [
                (
                  label: t.discoveryMeasuredModels,
                  value: '${data.total}',
                  caption: capabilityInfo(_capability).label(t),
                ),
                (
                  label: t.discoverySources,
                  value:
                      '${data.sources.where((source) => source.modelCount > 0).length}',
                  caption: null,
                ),
              ],
            ),
          ),
        TextField(
          controller: _search,
          decoration: InputDecoration(
            labelText: t.discoverySearchModels,
            prefixIcon: const Icon(Icons.search),
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
        LayoutBuilder(
          builder: (context, constraints) {
            final width = constraints.maxWidth < 560
                ? constraints.maxWidth
                : (constraints.maxWidth - 16) / 2;
            return Wrap(
              spacing: 16,
              runSpacing: 12,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                SizedBox(
                  width: width,
                  child: DropdownButtonFormField<String>(
                    initialValue: _capability,
                    decoration: InputDecoration(labelText: t.discoveryKind),
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
                  width: width,
                  child: DropdownButtonFormField<String>(
                    initialValue: _source,
                    isExpanded: true,
                    decoration: InputDecoration(
                      labelText: t.discoverySourceFilter,
                    ),
                    items: [
                      DropdownMenuItem(
                        value: '',
                        child: Text(t.discoveryAllSources),
                      ),
                      for (final source in data?.sources ?? <BenchmarkSource>[])
                        DropdownMenuItem(
                          value: source.id,
                          child: Text(
                            source.name,
                            overflow: TextOverflow.ellipsis,
                          ),
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
              ],
            );
          },
        ),
        BenchmarkComparison(
          key: _comparisonKey,
          models: _selected,
          sources: data?.sources ?? [],
          onRemove: _choose,
        ),
        if (_busy) const LinearProgressIndicator(),
        if (_error != null) InlineError(message: _error!),
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
              : SettingsCardList(
                  children: [
                    for (var i = 0; i < data.models.length; i++)
                      _BenchmarkRow(
                        key: ValueKey(data.models[i].id),
                        model: data.models[i],
                        rank: _offset + i + 1,
                        selected: _selected.any(
                          (model) => model.id == data.models[i].id,
                        ),
                        canSelect:
                            _selected.any(
                              (model) => model.id == data.models[i].id,
                            ) ||
                            (_selected.length < 2 &&
                                (_selected.isEmpty ||
                                    _selected.first.capability ==
                                        data.models[i].capability)),
                        onSelect: () => _choose(data.models[i]),
                      ),
                  ],
                ),
        ),
        if (data != null && data.total > 0)
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
              Flexible(
                child: Text(
                  t.discoveryPage(
                    '${_offset ~/ _pageSize + 1}',
                    '${data.total}',
                  ),
                  textAlign: TextAlign.center,
                ),
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
        const DiscoveryReveal(child: NeoRouterBenchmarkPreview()),
        AppCard(
          child: ExpansionTile(
            tilePadding: EdgeInsets.zero,
            title: Text(t.discoveryMethod),
            children: [
              Padding(
                padding: const EdgeInsets.only(bottom: 16),
                child: Text(
                  t.discoveryMethodHint,
                  style: TextStyle(color: palette.textSoft, height: 1.5),
                ),
              ),
            ],
          ),
        ),
        SectionCard(
          title: t.discoverySources,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                t.discoverySourceHint,
                style: TextStyle(color: palette.textSoft),
              ),
              const SizedBox(height: 12),
              for (final source in data?.sources ?? <BenchmarkSource>[])
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  title: Text(source.name),
                  subtitle: Text(
                    source.updatedAt == null
                        ? t.discoveryNotSynced
                        : t.discoveryUpdated(
                            discoveryDate(context, source.updatedAt!),
                          ),
                  ),
                  trailing: const Icon(Icons.open_in_new, size: 18),
                  onTap: () => launchUrl(
                    source.url,
                    mode: LaunchMode.externalApplication,
                  ),
                ),
            ],
          ),
        ),
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
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              SizedBox(
                width: 34,
                child: Text(
                  '$rank'.padLeft(2, '0'),
                  style: TextStyle(color: palette.textMuted, fontSize: 12),
                ),
              ),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      model.name,
                      style: TextStyle(
                        color: palette.text,
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      t.discoveryCoverage(
                        '${model.sourceCount}',
                        '${model.scores.length}',
                      ),
                      style: TextStyle(color: palette.textMuted, fontSize: 12),
                    ),
                    if (model.isFree)
                      Text(
                        t.discoveryFree,
                        style: TextStyle(color: palette.textSoft, fontSize: 11),
                      ),
                  ],
                ),
              ),
              const SizedBox(width: 8),
              Text(
                model.score.toStringAsFixed(1),
                style: TextStyle(
                  color: selected ? palette.accent : palette.text,
                  fontSize: 22,
                ),
              ),
              const SizedBox(width: 8),
              IconButton(
                tooltip: canSelect
                    ? selected
                          ? t.discoveryRemoveCompare(model.name)
                          : t.discoverySelectCompare(model.name)
                    : t.discoveryCompareLimit,
                onPressed: canSelect ? onSelect : null,
                icon: Icon(
                  selected
                      ? Icons.check_circle_outline
                      : Icons.add_circle_outline,
                  color: selected ? palette.accent : null,
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          Semantics(
            label:
                '${t.discoveryScore}: ${model.score.toStringAsFixed(1)} / 100',
            child: BenchmarkScoreBar(score: model.score, selected: selected),
          ),
        ],
      ),
    );
  }
}
