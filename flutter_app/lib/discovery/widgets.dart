import 'dart:ui' as ui;
import 'package:flutter/material.dart';
import 'package:intl/intl.dart';
import '../l10n/gen/app_localizations.dart';
import '../main_shared.dart';
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

/// Explicitly decorative preview. Contains no fabricated scores or model results.
class NeoRouterBenchmarkPreview extends StatelessWidget {
  const NeoRouterBenchmarkPreview({super.key});
  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return AppCard(
      padding: EdgeInsets.zero,
      child: Stack(
        alignment: Alignment.center,
        children: [
          ExcludeSemantics(
            child: IgnorePointer(
              child: ImageFiltered(
                imageFilter: ui.ImageFilter.blur(sigmaX: 6, sigmaY: 6),
                child: SizedBox(
                  height: 176,
                  child: Row(
                    children: [
                      for (final icon in [
                        Icons.science_outlined,
                        Icons.code_rounded,
                        Icons.psychology_outlined,
                      ])
                        Expanded(
                          child: Container(
                            margin: const EdgeInsets.all(16),
                            height: 100,
                            decoration: BoxDecoration(
                              color: palette.surfaceRaised,
                              borderRadius: BorderRadius.circular(14),
                            ),
                            child: Icon(
                              icon,
                              size: 36,
                              color: palette.textMuted,
                            ),
                          ),
                        ),
                    ],
                  ),
                ),
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.all(20),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(
                  Icons.lock_outline_rounded,
                  color: palette.accent,
                  size: 20,
                ),
                const SizedBox(height: 8),
                Text(
                  t.discoveryOwnTitle,
                  style: sectionTitleStyle(palette),
                  textAlign: TextAlign.center,
                ),
                const SizedBox(height: 6),
                Text(
                  t.discoveryOwnHint,
                  textAlign: TextAlign.center,
                  style: TextStyle(color: palette.textSoft),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
