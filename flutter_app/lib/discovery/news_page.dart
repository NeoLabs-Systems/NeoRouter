import 'dart:async';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';
import '../l10n/gen/app_localizations.dart';
import '../main_controller.dart';
import '../main_shared.dart';
import '../main_spacing.dart';
import '../main_theme.dart';
import 'models.dart';
import 'widgets.dart';

class ModelNewsScreen extends StatefulWidget {
  const ModelNewsScreen({super.key, required this.controller});
  final NeoRouterController controller;
  @override
  State<ModelNewsScreen> createState() => _ModelNewsScreenState();
}

class _ModelNewsScreenState extends State<ModelNewsScreen> {
  final _scroll = ScrollController();
  final _search = TextEditingController();
  final List<ModelNews> _items = [];
  Timer? _debounce;
  String? _error;
  DateTime? _updatedAt;
  int _total = 0, _request = 0;
  int? _asOf;
  bool _busy = true;
  bool get _hasMore => _items.length < _total;

  @override
  void initState() {
    super.initState();
    _scroll.addListener(_nearEnd);
    _load(reset: true);
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _scroll.dispose();
    _search.dispose();
    super.dispose();
  }

  void _nearEnd() {
    if (!_busy &&
        _error == null &&
        _hasMore &&
        _scroll.position.extentAfter < 500) {
      _load();
    }
  }

  Future<void> _load({bool reset = false}) async {
    final request = ++_request;
    setState(() {
      _busy = true;
      _error = null;
      if (reset) {
        _items.clear();
        _total = 0;
        _asOf = null;
      }
    });
    try {
      final page = await widget.controller.api.modelNews(
        query: _search.text,
        offset: _items.length,
        asOf: _asOf,
      );
      if (!mounted || request != _request) return;
      setState(() {
        final known = _items.map((item) => item.id).toSet();
        _items.addAll(page.items.where((item) => known.add(item.id)));
        _total = page.total;
        _asOf = page.asOf;
        _updatedAt = page.updatedAt;
      });
    } catch (error) {
      if (mounted && request == _request) setState(() => _error = '$error');
    } finally {
      if (mounted && request == _request) setState(() => _busy = false);
    }
  }

  // Days the person opened; today is always open.
  final Set<DateTime> _open = {};

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final today = DateUtils.dateOnly(DateTime.now());
    final days = <DateTime, List<ModelNews>>{today: []};
    for (final item in _items) {
      days
          .putIfAbsent(DateUtils.dateOnly(item.detectedAt.toLocal()), () => [])
          .add(item);
    }
    final ordered = days.keys.toList()..sort((a, b) => b.compareTo(a));
    // One flat list: a day row, then that day's models when it is open.
    final rows = <Widget>[];
    for (final day in ordered) {
      final items = days[day]!;
      final open = day == today || _open.contains(day);
      rows.add(
        _DayRow(
          label: day == today ? t.discoveryToday : discoveryDate(context, day),
          count: items.length,
          open: open,
          current: day == today,
          onTap: day == today
              ? null
              : () => setState(() => open ? _open.remove(day) : _open.add(day)),
        ),
      );
      if (day == today && items.isEmpty && !_busy) {
        rows.add(
          Padding(
            padding: const EdgeInsets.fromLTRB(28, 2, 0, 10),
            child: Text(
              t.discoveryNothingToday,
              style: TextStyle(color: palette.textMuted, fontSize: 12),
            ),
          ),
        );
      }
      if (open) {
        for (final item in items) {
          rows.add(
            DiscoveryReveal(
              key: ValueKey(item.id),
              child: _NewsRow(
                item: item,
                onBenchmarks: () => widget.controller.go(AppPage.benchmarks),
              ),
            ),
          );
        }
      }
    }
    // Lazily built rows: a long-lived timeline does not mount the whole feed.
    return ListView.builder(
      controller: _scroll,
      padding: pagePadding(context),
      itemCount: rows.length + 2,
      itemBuilder: (context, index) {
        Widget child;
        if (index == 0) {
          child = Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              ScreenHeader(
                title: t.discoveryNews,
                description: '',
                trailing: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Tooltip(
                      message:
                          '${t.discoveryNewsEmptyHint}\n\n${t.discoveryNewsRuleHint}',
                      child: Icon(
                        Icons.info_outline,
                        size: 18,
                        color: palette.textMuted,
                      ),
                    ),
                    IconButton(
                      tooltip: t.refresh,
                      onPressed: _busy ? null : () => _load(reset: true),
                      icon: const Icon(Icons.refresh),
                    ),
                  ],
                ),
              ),
              TextField(
                controller: _search,
                decoration: InputDecoration(
                  isDense: true,
                  hintText: t.discoveryNewsSearch,
                  prefixIcon: const Icon(Icons.search, size: 18),
                ),
                onChanged: (_) {
                  _request++;
                  _debounce?.cancel();
                  _debounce = Timer(
                    const Duration(milliseconds: 250),
                    () => _load(reset: true),
                  );
                },
              ),
              const SizedBox(height: 16),
            ],
          );
        } else if (index == rows.length + 1) {
          child = Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (_busy)
                const Padding(
                  padding: EdgeInsets.all(16),
                  child: Center(child: CircularProgressIndicator()),
                ),
              if (_error != null) ...[
                InlineError(message: _error!),
                TextButton(
                  onPressed: () => _load(reset: _items.isEmpty),
                  child: Text(t.retry),
                ),
              ],
              if (!_busy && _hasMore)
                Center(
                  child: TextButton(
                    onPressed: _load,
                    child: Text(t.discoveryMore),
                  ),
                ),
              if (_updatedAt != null)
                Padding(
                  padding: const EdgeInsets.only(top: 12),
                  child: Text(
                    t.discoveryUpdated(discoveryDate(context, _updatedAt!)),
                    textAlign: TextAlign.center,
                    style: TextStyle(color: palette.textMuted, fontSize: 11),
                  ),
                ),
            ],
          );
        } else {
          child = rows[index - 1];
        }
        return Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: kContentMaxWidth),
            child: child,
          ),
        );
      },
    );
  }
}

class _DayRow extends StatelessWidget {
  const _DayRow({
    required this.label,
    required this.count,
    required this.open,
    required this.current,
    this.onTap,
  });
  final String label;
  final int count;
  final bool open, current;
  final VoidCallback? onTap;
  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(8),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: Row(
          children: [
            SizedBox(
              width: 28,
              child: Icon(
                onTap == null
                    ? Icons.circle
                    : open
                    ? Icons.expand_more
                    : Icons.chevron_right,
                size: onTap == null ? 9 : 18,
                color: current ? palette.accent : palette.textMuted,
              ),
            ),
            Text(
              label,
              style: TextStyle(
                color: current ? palette.text : palette.textSoft,
                fontWeight: current ? FontWeight.w600 : FontWeight.w500,
                fontSize: 13,
              ),
            ),
            const SizedBox(width: 8),
            if (count > 0)
              Text(
                t.discoveryDayCount('$count'),
                style: TextStyle(color: palette.textMuted, fontSize: 12),
              ),
            const SizedBox(width: 12),
            Expanded(child: Divider(color: palette.border, height: 1)),
          ],
        ),
      ),
    );
  }
}

class _NewsRow extends StatelessWidget {
  const _NewsRow({required this.item, required this.onBenchmarks});
  final ModelNews item;
  final VoidCallback onBenchmarks;
  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.only(left: 28, bottom: 6),
      child: Material(
        color: palette.surface,
        borderRadius: BorderRadius.circular(10),
        child: InkWell(
          borderRadius: BorderRadius.circular(10),
          onTap: () =>
              launchUrl(item.url, mode: LaunchMode.externalApplication),
          child: Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
            decoration: BoxDecoration(
              border: Border.all(color: palette.border),
              borderRadius: BorderRadius.circular(10),
            ),
            child: Row(
              children: [
                Icon(
                  Icons.circle,
                  size: 7,
                  color: item.available ? palette.accent : palette.textMuted,
                ),
                const SizedBox(width: 10),
                Expanded(
                  flex: 3,
                  child: Tooltip(
                    message:
                        '${item.modelId}\n${t.discoveryConfirmed(discoveryDate(context, item.lastSeenAt))}',
                    child: Text(
                      item.name,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        color: item.available
                            ? palette.text
                            : palette.textMuted,
                        fontWeight: FontWeight.w500,
                        fontSize: 13,
                      ),
                    ),
                  ),
                ),
                if (item.contextWindow != null) ...[
                  const SizedBox(width: 8),
                  Text(
                    formatCount(item.contextWindow!),
                    style: TextStyle(color: palette.textMuted, fontSize: 11),
                  ),
                ],
                const SizedBox(width: 12),
                SizedBox(
                  width: 80,
                  child: BenchmarkScoreBar(score: item.score),
                ),
                SizedBox(
                  width: 36,
                  child: Text(
                    item.score.toStringAsFixed(0),
                    textAlign: TextAlign.right,
                    style: TextStyle(color: palette.text, fontSize: 12),
                  ),
                ),
                IconButton(
                  visualDensity: VisualDensity.compact,
                  iconSize: 16,
                  tooltip: t.discoveryOpenBenchmarks,
                  onPressed: onBenchmarks,
                  icon: Icon(Icons.bar_chart_rounded, color: palette.textMuted),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
