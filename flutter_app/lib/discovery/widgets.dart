import 'package:flutter/material.dart';
import 'package:intl/intl.dart';
import '../main_theme.dart';
import '../l10n/gen/app_localizations.dart';
import 'models.dart';

String discoveryDate(BuildContext context, DateTime date) => DateFormat.yMMMd(
  Localizations.localeOf(context).toLanguageTag(),
).format(date.toLocal());
String metricName(String id) => id
    .replaceAll('_', ' ')
    .replaceAllMapped(RegExp(r'\b\w'), (match) => match[0]!.toUpperCase());

/// Short, one-shot entrances. Never keeps ticking when a page is idle.
class DiscoveryReveal extends StatelessWidget {
  const DiscoveryReveal({super.key, required this.child});
  final Widget child;
  @override
  Widget build(BuildContext context) => TweenAnimationBuilder<double>(
    tween: Tween(begin: 0, end: 1),
    duration: MediaQuery.disableAnimationsOf(context)
        ? Duration.zero
        : const Duration(milliseconds: 280),
    curve: Curves.easeOutCubic,
    child: child,
    builder: (context, value, child) => Opacity(
      opacity: value,
      child: Transform.translate(
        offset: Offset(0, 8 * (1 - value)),
        child: child,
      ),
    ),
  );
}

class BenchmarkScoreBar extends StatelessWidget {
  const BenchmarkScoreBar({
    super.key,
    required this.score,
    this.selected = false,
  });
  final double score;
  final bool selected;
  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return TweenAnimationBuilder<double>(
      tween: Tween(begin: 0, end: score / 100),
      duration: MediaQuery.disableAnimationsOf(context)
          ? Duration.zero
          : const Duration(milliseconds: 450),
      curve: Curves.easeOutCubic,
      builder: (context, value, _) => ClipRRect(
        borderRadius: BorderRadius.circular(99),
        child: LinearProgressIndicator(
          value: value.clamp(0, 1),
          minHeight: 5,
          backgroundColor: palette.surfaceRaised,
          valueColor: AlwaysStoppedAnimation(
            selected ? palette.accent : palette.textSoft.withValues(alpha: .55),
          ),
        ),
      ),
    );
  }
}

class BenchmarkRankRow extends StatelessWidget {
  const BenchmarkRankRow({
    super.key,
    required this.model,
    required this.rank,
    required this.trailing,
    this.detail,
    this.selected = false,
    this.percentage = false,
    this.onTap,
  });

  final RankedBenchmark model;
  final int rank;
  final Widget trailing;
  final String? detail;
  final bool selected, percentage;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final score = model.score.toStringAsFixed(1);
    return InkWell(
      onTap: onTap,
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
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(
                          model.name,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(color: palette.text, fontSize: 13),
                        ),
                        if (detail != null)
                          Text(
                            detail!,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(
                              color: palette.textMuted,
                              fontSize: 11,
                            ),
                          ),
                      ],
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
                label: percentage
                    ? t.lbPreference(score)
                    : '${t.discoveryScore}: $score / 100',
                excludeSemantics: true,
                child: BenchmarkScoreBar(
                  score: model.score,
                  selected: selected,
                ),
              ),
            ),
            SizedBox(
              width: percentage ? 54 : 44,
              child: Text(
                percentage ? '$score%' : score,
                textAlign: TextAlign.right,
                style: TextStyle(
                  color: selected ? palette.accent : palette.text,
                  fontWeight: FontWeight.w500,
                  fontSize: 13,
                ),
              ),
            ),
            trailing,
          ],
        ),
      ),
    );
  }
}

class BenchmarkPagination extends StatelessWidget {
  const BenchmarkPagination({
    super.key,
    required this.offset,
    required this.pageSize,
    required this.total,
    required this.busy,
    required this.onPage,
  });
  final int offset, pageSize, total;
  final bool busy;
  final ValueChanged<int> onPage;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return Row(
      mainAxisAlignment: MainAxisAlignment.center,
      children: [
        IconButton(
          tooltip: t.discoveryPrevious,
          onPressed: busy || offset == 0
              ? null
              : () => onPage((offset - pageSize).clamp(0, total)),
          icon: const Icon(Icons.chevron_left),
        ),
        IconButton(
          tooltip: t.discoveryNext,
          onPressed: busy || offset + pageSize >= total
              ? null
              : () => onPage(offset + pageSize),
          icon: const Icon(Icons.chevron_right),
        ),
      ],
    );
  }
}
