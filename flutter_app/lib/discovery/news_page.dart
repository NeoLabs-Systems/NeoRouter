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

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    // Lazily built cards: a long-lived timeline does not mount the whole feed.
    return ListView.builder(
      controller: _scroll,
      padding: pagePadding(context),
      itemCount: _items.length + 2,
      itemBuilder: (context, index) {
        Widget child;
        if (index == 0) {
          child = Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              ScreenHeader(
                title: t.discoveryNews,
                description: t.discoveryNewsHint,
                trailing: IconButton(
                  tooltip: t.refresh,
                  onPressed: _busy ? null : () => _load(reset: true),
                  icon: const Icon(Icons.refresh),
                ),
              ),
              DiscoveryReveal(
                child: AppCard(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Icon(
                        Icons.auto_awesome_outlined,
                        size: 24,
                        color: palette.accent,
                      ),
                      const SizedBox(height: 12),
                      Text(
                        t.discoveryNewsRule,
                        style: sectionTitleStyle(palette),
                      ),
                      const SizedBox(height: 8),
                      Text(
                        t.discoveryNewsEmptyHint,
                        style: TextStyle(color: palette.textSoft, height: 1.5),
                      ),
                      const SizedBox(height: 8),
                      ExpansionTile(
                        tilePadding: EdgeInsets.zero,
                        title: Text(t.discoveryMethod),
                        children: [
                          Padding(
                            padding: const EdgeInsets.only(bottom: 12),
                            child: Text(
                              t.discoveryNewsRuleHint,
                              style: TextStyle(
                                color: palette.textMuted,
                                height: 1.5,
                              ),
                            ),
                          ),
                        ],
                      ),
                      if (_updatedAt != null)
                        Text(
                          t.discoveryUpdated(
                            discoveryDate(context, _updatedAt!),
                          ),
                          style: TextStyle(
                            color: palette.textMuted,
                            fontSize: 12,
                          ),
                        ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 24),
              TextField(
                controller: _search,
                decoration: InputDecoration(
                  labelText: t.discoveryNewsSearch,
                  prefixIcon: const Icon(Icons.search),
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
              const SizedBox(height: 28),
            ],
          );
        } else if (index == _items.length + 1) {
          child = Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (_busy)
                const Padding(
                  padding: EdgeInsets.all(24),
                  child: Center(child: CircularProgressIndicator()),
                ),
              if (_error != null) ...[
                InlineError(message: _error!),
                TextButton(
                  onPressed: () => _load(reset: _items.isEmpty),
                  child: Text(t.retry),
                ),
              ],
              if (!_busy && _error == null && _items.isEmpty)
                AppCard(
                  child: EmptyState(
                    icon: Icons.auto_awesome_outlined,
                    title: t.discoveryNewsEmpty,
                    message: t.discoveryNewsEmptyHint,
                  ),
                ),
              if (!_busy && _hasMore)
                Center(
                  child: OutlinedButton(
                    onPressed: _load,
                    child: Text(t.discoveryMore),
                  ),
                ),
            ],
          );
        } else {
          final position = index - 1;
          final item = _items[position];
          final day = DateUtils.dateOnly(item.detectedAt.toLocal());
          final previousDay = position == 0
              ? null
              : DateUtils.dateOnly(_items[position - 1].detectedAt.toLocal());
          child = Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (day != previousDay)
                Padding(
                  padding: const EdgeInsets.only(top: 8, bottom: 16),
                  child: Text(
                    discoveryDate(context, day),
                    style: TextStyle(
                      color: palette.textSoft,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                ),
              DiscoveryReveal(
                key: ValueKey(item.id),
                child: _NewsCard(
                  item: item,
                  onBenchmarks: () => widget.controller.go(AppPage.benchmarks),
                ),
              ),
            ],
          );
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

class _NewsCard extends StatelessWidget {
  const _NewsCard({required this.item, required this.onBenchmarks});
  final ModelNews item;
  final VoidCallback onBenchmarks;
  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return IntrinsicHeight(
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          SizedBox(
            width: 28,
            child: Column(
              children: [
                Container(
                  width: 9,
                  height: 9,
                  margin: const EdgeInsets.only(top: 22),
                  decoration: BoxDecoration(
                    shape: BoxShape.circle,
                    color: item.available ? palette.accent : palette.textMuted,
                  ),
                ),
                Expanded(child: Container(width: 1, color: palette.border)),
              ],
            ),
          ),
          Expanded(
            child: Padding(
              padding: const EdgeInsets.only(bottom: 20),
              child: AppCard(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Wrap(
                      spacing: 8,
                      runSpacing: 8,
                      children: [
                        MetaPill(
                          label: item.available
                              ? t.discoveryFree
                              : t.discoveryUnavailable,
                          icon: item.available
                              ? Icons.check_circle_outline
                              : Icons.history,
                        ),
                        if (item.contextWindow != null)
                          MetaPill(
                            label: t.discoveryContext(
                              formatCount(item.contextWindow!),
                            ),
                          ),
                      ],
                    ),
                    const SizedBox(height: 14),
                    Text(
                      item.name,
                      style: TextStyle(
                        fontSize: 21,
                        color: palette.text,
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                    const SizedBox(height: 6),
                    Text(
                      item.modelId,
                      style: TextStyle(color: palette.textMuted, fontSize: 12),
                    ),
                    const SizedBox(height: 16),
                    Text(
                      t.discoveryNewsScore(
                        item.score.toStringAsFixed(1),
                        '${item.sourceCount}',
                      ),
                      style: TextStyle(color: palette.textSoft),
                    ),
                    const SizedBox(height: 10),
                    BenchmarkScoreBar(score: item.score),
                    const SizedBox(height: 14),
                    Text(
                      t.discoveryDetected(
                        discoveryDate(context, item.detectedAt),
                      ),
                      style: TextStyle(color: palette.textMuted, fontSize: 12),
                    ),
                    Text(
                      t.discoveryConfirmed(
                        discoveryDate(context, item.lastSeenAt),
                      ),
                      style: TextStyle(color: palette.textMuted, fontSize: 12),
                    ),
                    const SizedBox(height: 12),
                    Wrap(
                      spacing: 12,
                      runSpacing: 8,
                      children: [
                        OutlinedButton.icon(
                          onPressed: () => launchUrl(
                            item.url,
                            mode: LaunchMode.externalApplication,
                          ),
                          icon: const Icon(Icons.open_in_new, size: 16),
                          label: Text(t.discoveryOpenModel),
                        ),
                        TextButton(
                          onPressed: onBenchmarks,
                          child: Text(t.discoveryOpenBenchmarks),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
