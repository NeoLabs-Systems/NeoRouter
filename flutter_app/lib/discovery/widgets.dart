import 'package:flutter/material.dart';
import 'package:intl/intl.dart';
import '../main_theme.dart';

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
